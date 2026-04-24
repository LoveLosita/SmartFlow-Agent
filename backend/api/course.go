package api

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/service"
	"github.com/gin-gonic/gin"
)

type CourseHandler struct {
	service *service.CourseService
}

func NewCourseHandler(service *service.CourseService) *CourseHandler {
	return &CourseHandler{
		service: service,
	}
}

func (sa *CourseHandler) CheckUserCourse(c *gin.Context) {
	var req model.UserCheckCourseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	if service.CheckSingleCourse(req) {
		c.JSON(http.StatusOK, respond.Ok)
		return
	}
	c.JSON(http.StatusBadRequest, respond.WrongCourseInfo)
}

func (sa *CourseHandler) AddUserCourses(c *gin.Context) {
	var req model.UserImportCoursesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}

	userID := c.GetInt("user_id")
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	conflicts, err := sa.service.AddUserCourses(ctx, req, userID)
	if err != nil {
		if errors.Is(err, respond.ScheduleConflict) {
			c.JSON(http.StatusConflict, respond.RespWithData(respond.ScheduleConflict, conflicts))
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

	draft, err := sa.service.ParseCourseTableImage(ctx, model.CourseImageParseRequest{
		Filename:   fileHeader.Filename,
		MIMEType:   fileHeader.Header.Get("Content-Type"),
		ImageBytes: imageBytes,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrCourseImageParserUnavailable):
			log.Printf("[COURSE_PARSE][API] parser unavailable user=%d filename=%q", userID, fileHeader.Filename)
			c.JSON(http.StatusServiceUnavailable, respond.Response{Status: "50003", Info: "course image parser is not configured"})
			return
		case errors.Is(err, service.ErrCourseImageTooLarge):
			log.Printf("[COURSE_PARSE][API] file too large user=%d filename=%q bytes=%d", userID, fileHeader.Filename, len(imageBytes))
			c.JSON(http.StatusBadRequest, respond.Response{Status: "40064", Info: "course image too large"})
			return
		case errors.Is(err, service.ErrCourseImageUnsupportedMIME):
			log.Printf(
				"[COURSE_PARSE][API] unsupported mime user=%d filename=%q header_content_type=%q",
				userID,
				fileHeader.Filename,
				fileHeader.Header.Get("Content-Type"),
			)
			c.JSON(http.StatusBadRequest, respond.Response{Status: "40065", Info: "unsupported course image format"})
			return
		case errors.Is(err, service.ErrCourseImageEmpty):
			log.Printf("[COURSE_PARSE][API] empty file user=%d filename=%q", userID, fileHeader.Filename)
			c.JSON(http.StatusBadRequest, respond.Response{Status: "40066", Info: "course image is empty"})
			return
		default:
			log.Printf("[COURSE_PARSE][API] unexpected failure user=%d filename=%q err=%v", userID, fileHeader.Filename, err)
			respond.DealWithError(c, err)
			return
		}
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
