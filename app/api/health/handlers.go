//go:build legacy

package health

import "github.com/gin-gonic/gin"

func (h *healthRouter) health(ctx *gin.Context) {
	ctx.JSON(200, gin.H{
		"status": "OK",
	})
}
