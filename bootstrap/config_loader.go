package bootstrap

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"time"

	"github.com/wssto2/go-core/validation"
)

// LoadConfig populates the given struct pointer from environment variables and validates it.
func LoadConfig(cfg any) error {
	rv := reflect.ValueOf(cfg)
	if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("bootstrap: LoadConfig requires a pointer to a struct, got %T", cfg)
	}

	if err := populate(rv.Elem()); err != nil {
		return err
	}

	return validateDeep(rv.Elem())
}

// validateDeep validates a struct value and recurses into nested struct fields.
func validateDeep(rv reflect.Value) error {
	ptr := reflect.New(rv.Type())
	ptr.Elem().Set(rv)
	v := validation.New()
	if err := v.Validate(ptr.Interface()); err != nil {
		return err
	}
	for i := 0; i < rv.NumField(); i++ {
		field := rv.Field(i)
		if field.Kind() == reflect.Struct && field.Type() != reflect.TypeOf(time.Time{}) {
			if err := validateDeep(field); err != nil {
				return err
			}
		}
	}
	return nil
}

func populate(rv reflect.Value) error {
	rt := rv.Type()

	for i := 0; i < rv.NumField(); i++ {
		field := rv.Field(i)
		fieldType := rt.Field(i)

		// Populating from env tag
		envTag := fieldType.Tag.Get("env")
		if envTag != "" {
			envVal := os.Getenv(envTag)
			if envVal != "" {
				if err := setField(field, envVal); err != nil {
					return fmt.Errorf("bootstrap: failed to set field %s from env %s: %w", fieldType.Name, envTag, err)
				}
			}
		}

		// Handle pointer-to-struct: initialise and recurse
		f := field
		if f.Kind() == reflect.Ptr {
			if f.IsNil() {
				f.Set(reflect.New(f.Type().Elem()))
			}
			f = f.Elem()
		}

		// Recurse into structs, skip time.Duration (int64 alias)
		if f.Kind() == reflect.Struct && f.Type() != reflect.TypeOf(time.Time{}) {
			if err := populate(f); err != nil {
				return err
			}
		}
	}

	return nil
}

func setField(field reflect.Value, val string) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(val)
	case reflect.Int:
		i, err := strconv.Atoi(val)
		if err != nil {
			return err
		}
		field.SetInt(int64(i))
	case reflect.Bool:
		b, err := strconv.ParseBool(val)
		if err != nil {
			return err
		}
		field.SetBool(b)
	case reflect.Int64:
		if field.Type() == reflect.TypeOf(time.Second) {
			d, err := time.ParseDuration(val)
			if err != nil {
				// Fallback to integer (seconds)
				sec, errSec := strconv.Atoi(val)
				if errSec != nil {
					return err
				}
				d = time.Duration(sec) * time.Second
			}
			field.SetInt(int64(d))
		} else {
			i, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return err
			}
			field.SetInt(i)
		}
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return err
		}
		field.SetFloat(f)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := strconv.ParseUint(val, 10, 64)
		if err != nil {
			return err
		}
		field.SetUint(u)
	default:
		return fmt.Errorf("unsupported type: %s", field.Kind())
	}
	return nil
}
