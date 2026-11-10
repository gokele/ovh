package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

// Unknown constraints must fail before ordering instead of silently becoming defaults.
func bindOrderJSON(c *gin.Context, value interface{}) error {
	if c.Request == nil || c.Request.Body == nil {
		return errors.New("order request body is required")
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return errors.New("order request must contain one JSON value")
	}
	return binding.Validator.ValidateStruct(value)
}

// rejectAutoPay 自动付款功能已删除。旧页面仍可能带 autoPay 字段:false 照常接受,
// true 明确拒绝并说明原因,不会悄悄按"不付款"下单让用户以为已自动付款。
// 返回 true 表示已写出 400 响应,调用方应直接返回。
func rejectAutoPay(c *gin.Context, requested *bool) bool {
	if requested == nil || !*requested {
		return false
	}
	c.JSON(http.StatusBadRequest, gin.H{"status": "error", "code": "AUTO_PAY_REMOVED",
		"error": "自动付款功能已移除,请刷新页面后重试;下单后请在 OVH 订单页自行付款"})
	return true
}
