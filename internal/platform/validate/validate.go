// Package validate wraps go-playground/validator with API-friendly messages.
package validate

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

var instance *validator.Validate

func init() {
	instance = validator.New(validator.WithRequiredStructEnabled())
	instance.RegisterTagNameFunc(func(field reflect.StructField) string {
		name := strings.SplitN(field.Tag.Get("json"), ",", 2)[0]
		if name == "" || name == "-" {
			return field.Name
		}
		return name
	})
}

// Struct returns a field -> message map, or nil when input is valid.
func Struct(input any) map[string]string {
	err := instance.Struct(input)
	if err == nil {
		return nil
	}
	var validationErrs validator.ValidationErrors
	if !errors.As(err, &validationErrs) {
		return map[string]string{"_": err.Error()}
	}
	details := make(map[string]string, len(validationErrs))
	for _, fieldErr := range validationErrs {
		details[fieldErr.Field()] = message(fieldErr)
	}
	return details
}

func Validator() *validator.Validate { return instance }

func message(fieldErr validator.FieldError) string {
	param := fieldErr.Param()
	switch fieldErr.Tag() {
	case "required", "required_if", "required_with", "required_without":
		return "is required"
	case "email":
		return "must be a valid email address"
	case "url":
		return "must be a valid URL"
	case "uuid", "uuid4":
		return "must be a valid UUID"
	case "min":
		if fieldErr.Kind() == reflect.Slice || fieldErr.Kind() == reflect.Map {
			return fmt.Sprintf("must contain at least %s item(s)", param)
		}
		if fieldErr.Kind() == reflect.Int || fieldErr.Kind() == reflect.Int64 || fieldErr.Kind() == reflect.Float64 {
			return fmt.Sprintf("must be at least %s", param)
		}
		return fmt.Sprintf("must be at least %s characters", param)
	case "max":
		if fieldErr.Kind() == reflect.Slice || fieldErr.Kind() == reflect.Map {
			return fmt.Sprintf("must contain at most %s item(s)", param)
		}
		if fieldErr.Kind() == reflect.Int || fieldErr.Kind() == reflect.Int64 || fieldErr.Kind() == reflect.Float64 {
			return fmt.Sprintf("must be at most %s", param)
		}
		return fmt.Sprintf("must be at most %s characters", param)
	case "len":
		return fmt.Sprintf("must be exactly %s characters", param)
	case "gte":
		return fmt.Sprintf("must be greater than or equal to %s", param)
	case "lte":
		return fmt.Sprintf("must be less than or equal to %s", param)
	case "gt":
		return fmt.Sprintf("must be greater than %s", param)
	case "lt":
		return fmt.Sprintf("must be less than %s", param)
	case "oneof":
		return "must be one of: " + strings.ReplaceAll(param, " ", ", ")
	case "datetime":
		return "must use format " + param
	case "alphanum":
		return "must contain only letters and numbers"
	case "numeric":
		return "must be numeric"
	case "e164":
		return "must be a valid E.164 phone number"
	default:
		return "is invalid"
	}
}
