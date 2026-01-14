package httputil

import (
	"sync"

	"github.com/go-playground/validator/v10"
)

var (
	validatorOnce sync.Once
	validatorInst *validator.Validate
)

func ValidateStruct(v any) error {
	validatorOnce.Do(func() {
		validatorInst = validator.New()
	})
	return validatorInst.Struct(v)
}
