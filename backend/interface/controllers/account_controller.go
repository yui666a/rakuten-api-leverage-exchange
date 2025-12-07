package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/domain"
)

// AccountController handles account-related HTTP requests
type AccountController struct {
	accountRepo domain.AccountRepository
}

// NewAccountController creates a new AccountController instance
func NewAccountController(accountRepo domain.AccountRepository) *AccountController {
	return &AccountController{
		accountRepo: accountRepo,
	}
}

// GetAccount handles GET /api/accounts/:id
func (c *AccountController) GetAccount(ctx *gin.Context) {
	id := ctx.Param("id")

	account, err := c.accountRepo.GetAccount(ctx.Request.Context(), id)
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{
			"error": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, account)
}
