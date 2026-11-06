package handlers

import (
	"encoding/json"
	"errors"
	"io"

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
