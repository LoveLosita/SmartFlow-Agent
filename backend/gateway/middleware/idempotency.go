package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/LoveLosita/smartflow/backend/gateway/shared/respond"
	"github.com/LoveLosita/smartflow/backend/services/runtime/dao"
	"github.com/gin-gonic/gin"
)

type IdempotencyValue struct {
	Status int    `json:"status"` // HTTP 状态码
	Body   string `json:"body"`   // JSON 响应体
}

type responseRecorder struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)                  // 录制到缓冲区
	return r.ResponseWriter.Write(b) // 正常发送给前端
}

func IdempotencyMiddleware(cache *dao.CacheDAO) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 获取 IKey
		ikey := c.GetHeader("X-Idempotency-Key")
		if ikey == "" {
			c.JSON(http.StatusBadRequest, respond.MissingIdempotencyKey) // 400 错误，缺少 IKey
			c.Abort()
			return
		}

		userID := c.GetInt("user_id") // 假设 JWT 已存入
		redisKey := fmt.Sprintf("idempotency:%d:%s", userID, ikey)

		// 2. 查 Redis 缓存
		cachedData, err := cache.GetRecord(c, redisKey)
		if err != nil { // 💡 Fail-Open：Redis 挂了也别卡住用户，记个日志继续走
			log.Printf("[Idempotency] Redis Get error: %v", err)
		} else if cachedData != "" {
			// 命中缓存，直接回放录像
			var val IdempotencyValue
			json.Unmarshal([]byte(cachedData), &val)
			c.Data(val.Status, "application/json", []byte(val.Body))
			c.Abort()
			return
		}

		// 3. 分布式锁：防止微秒级的并发碰撞 (SetNX)
		// 锁 10 秒，防止请求卡死导致 key 永久锁定
		lockKey := redisKey + ":lock"
		success, err := cache.AcquireLock(c, lockKey, 10*time.Second)
		if err != nil { // 如果加锁报错，为了保险我们依然放行，让底层的数据库唯一索引去兜底
			log.Printf("[Idempotency] Redis Lock error: %v", err)
		} else if !success {
			c.JSON(http.StatusConflict, respond.RequestIsProcessing)
			c.Abort()
			return
		}
		// 💡 只有在加锁成功时才需要 defer 删锁
		if err == nil && success {
			defer cache.ReleaseLock(c, lockKey)
		}

		// 4. 装饰 ResponseWriter 开始录制
		recorder := &responseRecorder{
			ResponseWriter: c.Writer,
			body:           bytes.NewBufferString(""),
		}
		c.Writer = recorder

		// 5. 执行后续 Handler (你的 Service 逻辑)
		c.Next()

		// 6. 录制完成，存入 Redis (缓存 24 小时)
		// 只有状态码 < 500 时才存入 Redis，这样如果是服务器临时抽风，用户重试依然有机会成功
		if c.Writer.Status() < 500 {
			respVal := IdempotencyValue{
				Status: c.Writer.Status(),
				Body:   recorder.body.String(),
			}
			data, _ := json.Marshal(respVal)
			if err := cache.SaveRecord(c, redisKey, string(data), 24*time.Hour); err != nil {
				log.Printf("[Idempotency] Redis Save error: %v", err)
			}
		}
	}
}
