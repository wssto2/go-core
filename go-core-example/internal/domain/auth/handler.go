package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/web"
)

type handler struct {
	svc Service
}

func newHandler(svc Service) *handler {
	return &handler{svc: svc}
}

// LoginRequest is the payload accepted by POST /api/v1/auth/login.
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// login handles POST /api/v1/auth/login.
func (h *handler) login(ctx *gin.Context) {
	var req LoginRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		web.Fail(ctx, apperr.BadRequest("username and password are required"))
		return
	}

	token, err := h.svc.Login(ctx.Request.Context(), req.Username, req.Password)
	if err != nil {
		web.Fail(ctx, err)
		return
	}

	web.JSON(ctx, http.StatusOK, gin.H{"token": token}, nil)
}
