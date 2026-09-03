package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/example/url-shortener/internal/model"
	"github.com/example/url-shortener/internal/service"
)

type URLHandler struct {
	service *service.URLService
}

func NewURLHandler(svc *service.URLService) *URLHandler {
	return &URLHandler{service: svc}
}

// Shorten godoc
// POST /api/v1/shorten
func (h *URLHandler) Shorten(c *gin.Context) {
	var req model.CreateURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	resp, err := h.service.Shorten(c.Request.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrAlreadyExists):
			c.JSON(http.StatusConflict, gin.H{"error": "custom alias already exists"})
		case errors.Is(err, model.ErrInvalidURL):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid URL or alias"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to shorten url"})
		}
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// Redirect godoc
// GET /:code -> 302 redirect to long URL
func (h *URLHandler) Redirect(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing code"})
		return
	}

	longURL, err := h.service.Resolve(c.Request.Context(), code)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "short url not found"})
		case errors.Is(err, model.ErrExpired):
			c.JSON(http.StatusGone, gin.H{"error": "short url expired"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve"})
		}
		return
	}

	c.Redirect(http.StatusFound, longURL)
}

// Stats godoc
// GET /api/v1/stats/:code
func (h *URLHandler) Stats(c *gin.Context) {
	code := c.Param("code")
	u, err := h.service.Stats(c.Request.Context(), code)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch stats"})
		return
	}
	c.JSON(http.StatusOK, u)
}

// Health godoc
// GET /health
func Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
