//go:build legacy

package client

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func (hdl *clientRouter) handleRedirect(c *gin.Context) {
	shortCode := c.Param("shortCode")

	url, err := hdl.urlSvc.LookupUrl(shortCode)

	if err != nil {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.Redirect(http.StatusPermanentRedirect, url)
}

func (hdl *clientRouter) handleVerification(c *gin.Context) {
	verificationCode := c.Query("v")

	if len(verificationCode) == 0 {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	user, err := hdl.userSvc.GetByVerificationToken(verificationCode)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid verification token"})
		return
	}

	if time.Until(user.VerificationExpires) < 1 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Token expired"})
		return
	}

	if err := hdl.userSvc.SetVerified(user.ID); err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	c.Status(http.StatusOK)
}
