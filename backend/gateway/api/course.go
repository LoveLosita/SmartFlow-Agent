package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/LoveLosita/smartflow/backend/gateway/shared/respond"
	coursecontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/course"
	"github.com/LoveLosita/smartflow/backend/shared/ports"
	"github.com/gin-gonic/gin"
)

// 课表导入与校验可能涉及较多课程展开与冲突检测，统一放宽到 5 分钟，避免网关提前超时。
const courseRequestTimeout = 5 * time.Minute

type CourseHandler struct {
	client ports.CourseCommandClient
}

func NewCourseHandler(client ports.CourseCommandClient) *CourseHandler {
	return &CourseHandler{client: client}
}

type courseImportConflict interface {
	ConflictsJSON() json.RawMessage
}

func (sa *CourseHandler) CheckUserCourse(c *gin.Context) {
	var req coursecontracts.UserCheckCourseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), courseRequestTimeout)
	defer cancel()
	if err := sa.client.ValidateCourse(ctx, req); err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.Ok)
}

func (sa *CourseHandler) AddUserCourses(c *gin.Context) {
	var req coursecontracts.UserImportCoursesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	req.UserID = c.GetInt("user_id")
	ctx, cancel := context.WithTimeout(c.Request.Context(), courseRequestTimeout)
	defer cancel()

	_, err := sa.client.ImportCourses(ctx, req)
	if err != nil {
		var conflict courseImportConflict
		if errors.As(err, &conflict) {
			c.JSON(http.StatusConflict, respond.RespWithData(respond.ScheduleConflict, conflict.ConflictsJSON()))
			return
		}
		respond.DealWithError(c, err)
		return
	}

	c.JSON(http.StatusOK, respond.Ok)
}

func (sa *CourseHandler) ParseCourseTableImage(c *gin.Context) {
	userID := c.GetInt("user_id")
	fileHeader, err := c.FormFile("image")
	if err != nil {
		log.Printf("[COURSE_PARSE][API] missing file user=%d path=%s err=%v", userID, c.FullPath(), err)
		c.JSON(http.StatusBadRequest, respond.MissingParam)
		return
	}

	log.Printf(
		"[COURSE_PARSE][API] request start user=%d path=%s filename=%q header_content_type=%q size=%d",
		userID,
		c.FullPath(),
		fileHeader.Filename,
		fileHeader.Header.Get("Content-Type"),
		fileHeader.Size,
	)

	file, err := fileHeader.Open()
	if err != nil {
		log.Printf("[COURSE_PARSE][API] open file failed user=%d filename=%q err=%v", userID, fileHeader.Filename, err)
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	defer file.Close()

	imageBytes, err := io.ReadAll(file)
	if err != nil {
		log.Printf("[COURSE_PARSE][API] read file failed user=%d filename=%q err=%v", userID, fileHeader.Filename, err)
		respond.DealWithError(c, err)
		return
	}

	log.Printf(
		"[COURSE_PARSE][API] file loaded user=%d filename=%q bytes=%d",
		userID,
		fileHeader.Filename,
		len(imageBytes),
	)

	// 课表图片识别当前不再额外叠加服务端 45 秒超时，避免长耗时多模态请求被本层提前打断。
	// 这里只跟随客户端请求上下文，便于先观察真实上游耗时与失败位置。
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	rawDraft, err := sa.client.ParseCourseTableImage(ctx, coursecontracts.CourseImageParseRequest{
		UserID:     userID,
		Filename:   fileHeader.Filename,
		MIMEType:   fileHeader.Header.Get("Content-Type"),
		ImageBytes: imageBytes,
	})
	if err != nil {
		var resp respond.Response
		switch {
		case errors.As(err, &resp) && resp.Status == "50003":
			log.Printf("[COURSE_PARSE][API] parser unavailable user=%d filename=%q", userID, fileHeader.Filename)
			c.JSON(http.StatusServiceUnavailable, resp)
			return
		case errors.As(err, &resp) && resp.Status == "40064":
			log.Printf("[COURSE_PARSE][API] file too large user=%d filename=%q bytes=%d", userID, fileHeader.Filename, len(imageBytes))
			c.JSON(http.StatusBadRequest, resp)
			return
		case errors.As(err, &resp) && resp.Status == "40065":
			log.Printf(
				"[COURSE_PARSE][API] unsupported mime user=%d filename=%q header_content_type=%q",
				userID,
				fileHeader.Filename,
				fileHeader.Header.Get("Content-Type"),
			)
			c.JSON(http.StatusBadRequest, resp)
			return
		case errors.As(err, &resp) && resp.Status == "40066":
			log.Printf("[COURSE_PARSE][API] empty file user=%d filename=%q", userID, fileHeader.Filename)
			c.JSON(http.StatusBadRequest, resp)
			return
		default:
			log.Printf("[COURSE_PARSE][API] unexpected failure user=%d filename=%q err=%v", userID, fileHeader.Filename, err)
			respond.DealWithError(c, err)
			return
		}
	}

	var draft coursecontracts.CourseImageParseResponse
	if err := json.Unmarshal(rawDraft, &draft); err != nil {
		log.Printf("[COURSE_PARSE][API] decode response failed user=%d filename=%q err=%v", userID, fileHeader.Filename, err)
		respond.DealWithError(c, err)
		return
	}

	log.Printf(
		"[COURSE_PARSE][API] request success user=%d filename=%q draft_status=%s rows=%d warnings=%d",
		userID,
		fileHeader.Filename,
		draft.DraftStatus,
		len(draft.Rows),
		len(draft.Warnings),
	)
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, draft))
}
