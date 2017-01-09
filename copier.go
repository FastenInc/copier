package copier

import (
	"fmt"

	"reflect"

	log "github.com/Sirupsen/logrus"
)

func Copy(toValue interface{}, fromValue interface{}) (err error) {
	var (
		isSlice bool
		amount  int
	)
	var accumulatedError error

	from := reflect.ValueOf(fromValue)
	to := reflect.ValueOf(toValue)

	log.Infof("COPY toValue = %+v (%+v) fromValue = %+v (%+v)", toValue, from.Kind(), fromValue, to.Kind())

	if to.Kind() == reflect.Ptr && to.Elem().Kind() != reflect.Slice {
		return Copy(to.Elem().Addr(), from.Elem().Addr())
	}

	if from.Kind() == reflect.Slice {
		return fmt.Errorf("cannot copy slice by-value, try to pass it by pointer")
	} else if from.Kind() == reflect.Ptr && from.Elem().Kind() == reflect.Slice {
		isSlice = true
		amount = from.Elem().Len()
	} else {
		amount = 1
	}

	log.Infof("amount = %d isSlice = %t", amount, isSlice)

	if isSlice {
		if to.IsNil() {
			log.Infof("[IsNil]")
			to.Set(reflect.MakeSlice(to.Type(), 0, amount))
		}
		if from.Kind() == reflect.Slice {
			newSlice := reflect.MakeSlice(to.Type(), amount, amount)
			originalLen := to.Len()
			log.Infof("[11123] to = %+v newSlice = %+v reflect.AppendSlice(to, newSlice) = %+v", to, newSlice, reflect.AppendSlice(to, newSlice))
			to.Set(reflect.AppendSlice(to, newSlice))
			if from.Type().Elem().Kind() == reflect.Ptr {

				for i := 0; i < amount; i++ {
					var newT reflect.Value
					if to.Type().Elem().Kind() == reflect.Ptr {
						newT = reflect.New(to.Type().Elem().Elem())
					} else {
						newT = reflect.New(to.Type().Elem())
					}
					log.Infof("FROM = %+v", from.Index(i))
					err := Copy(newT.Interface(), from.Index(i))
					to.Index(originalLen + i).Set(newT)
					if nil != err {
						if nil == accumulatedError {
							accumulatedError = err
							continue
						}
						accumulatedError = fmt.Errorf("error copying %v\n%v", err, accumulatedError)
					}
				}
			} else if from.Type().Elem().Kind() == reflect.Struct {
				for i := 0; i < amount; i++ {
					err := Copy(to.Index(originalLen+i).Addr().Interface(), from.Index(i).Addr().Interface())
					if nil != err {
						if nil == accumulatedError {
							accumulatedError = err
							continue
						}
						accumulatedError = fmt.Errorf("error copying %v\n%v", err, accumulatedError)
					}
				}
			} else {
				reflect.Copy(to, from)
			}
		} else if from.Kind() == reflect.Struct {
			newSlice := reflect.MakeSlice(to.Type(), 1, 1)
			var newT reflect.Value
			if to.Type().Elem().Kind() == reflect.Ptr {
				newT = reflect.New(to.Type().Elem().Elem())
				newSlice.Index(0).Set(newT)
			} else {
				newT = reflect.New(to.Type().Elem())
				newSlice.Index(0).Set(newT.Elem())
			}
			originalLen := to.Len()
			to.Set(reflect.AppendSlice(to, newSlice))
			if to.Type().Elem().Kind() == reflect.Ptr {
				return Copy(to.Index(originalLen).Addr().Interface(), from.Addr().Interface())
			}

			return Copy(to.Index(originalLen).Addr().Interface(), from.Addr().Interface())
		} else if from.Kind() == reflect.Ptr {
			return Copy(toValue, from.Elem().Interface())
		}

		return fmt.Errorf("source slice type unsupported\n%v", accumulatedError)
	}

	for _, name := range deepFields(reflect.ValueOf(toValue).Type()) {
		log.Infof("name = %+v", name)
		fromField := fieldByName(from, name)
		fromMethod := methodByName(from, name)
		toField := fieldByName(to, name)
		toMethod := methodByName(to, name)

		canCopy := fromField.IsValid() && toMethod.IsValid() &&
			toMethod.Type().NumIn() == 1 && fromField.Type().AssignableTo(toMethod.Type().In(0))
		if canCopy {
			toMethod.Call([]reflect.Value{fromField})
			continue
		}

		canCopy = fromMethod.IsValid() && toField.IsValid() &&
			fromMethod.Type().NumOut() == 1 && fromMethod.Type().Out(0).AssignableTo(toField.Type())
		if canCopy {
			toField.Set(fromMethod.Call([]reflect.Value{})[0])
			continue
		}

		if fromMethod.IsValid() && toMethod.IsValid() {
		}
		canCopy = fromMethod.IsValid() && toMethod.IsValid() &&
			toMethod.Type().NumIn() == 1 && fromMethod.Type().NumOut() == 1 &&
			fromMethod.Type().Out(0).AssignableTo(toMethod.Type().In(0))
		if canCopy {
			toMethod.Call(fromMethod.Call([]reflect.Value{}))
			continue
		}

		_, accumulatedError = copyValue(toField, fromField, accumulatedError)
	}
	return accumulatedError
}

func fieldByName(base reflect.Value, name string) reflect.Value {
	if base.Kind() == reflect.Ptr {
		return fieldByName(base.Elem(), name)
	}
	if base.Kind() == reflect.Struct {
		return base.FieldByName(name)
	}
	return reflect.Zero(base.Type())
}

func methodByName(base reflect.Value, name string) reflect.Value {
	if base.Kind() == reflect.Ptr {
		if base.Elem().Kind() == reflect.Struct {
			result := base.MethodByName(name)
			if result.IsValid() {
				return result
			}
		}
		return methodByName(base.Elem(), name)
	}
	if base.Kind() == reflect.Struct {
		return base.MethodByName(name)
	}
	return reflect.Zero(base.Type())
}

func copyValue(to reflect.Value, from reflect.Value, accumulatedError error) (bool, error) {
	log.Infof("copyValue to %+v from %+v", to, from)
	fieldsAreValid := to.IsValid() && from.IsValid()
	canCopy := fieldsAreValid && to.CanSet() && from.Type().AssignableTo(to.Type())

	if canCopy {
		to.Set(from)
		return true, accumulatedError
	}

	if !fieldsAreValid {
		return false, accumulatedError
	}

	_, accumulatedError = tryDeepCopyPtr(to, from, accumulatedError)
	_, accumulatedError = tryDeepCopyStruct(to, from, accumulatedError)
	_, accumulatedError = tryDeepCopySlice(to, from, accumulatedError)

	return false, accumulatedError
}

func tryDeepCopyPtr(toField reflect.Value, fromField reflect.Value, accumulatedError error) (bool, error) {
	deepCopyRequired := toField.Type().Kind() == reflect.Ptr && fromField.Type().Kind() == reflect.Ptr &&
		!fromField.IsNil() && toField.CanSet()

	copied := false
	if deepCopyRequired {
		toType := toField.Type().Elem()
		emptyObject := reflect.New(toType)
		toField.Set(emptyObject)
		err := Copy(toField.Interface(), fromField.Interface())
		if nil != err {
			copied = false
			if nil == accumulatedError {
				accumulatedError = err
				return false, accumulatedError
			}
			accumulatedError = fmt.Errorf("error copying %v\n%v", err, accumulatedError)
		} else {
			copied = true
		}
	}
	return copied, accumulatedError
}

func tryDeepCopyStruct(toField reflect.Value, fromField reflect.Value, accumulatedError error) (bool, error) {
	deepCopyRequired := toField.Type().Kind() == reflect.Struct && fromField.Type().Kind() == reflect.Struct && toField.CanSet()

	copied := false
	if deepCopyRequired {
		err := Copy(toField.Addr().Interface(), fromField.Addr().Interface())
		if nil != err {
			copied = false
			if nil == accumulatedError {
				accumulatedError = err
				return false, accumulatedError
			}
			accumulatedError = fmt.Errorf("error copying %v\n%v", err, accumulatedError)
		} else {
			copied = true
		}
	}
	return copied, accumulatedError
}

func tryDeepCopySlice(toField reflect.Value, fromField reflect.Value, accumulatedError error) (bool, error) {
	deepCopyRequired := toField.Type().Kind() == reflect.Slice && fromField.Type().Kind() == reflect.Slice && toField.CanSet()

	copied := false
	if deepCopyRequired {
		err := Copy(toField.Addr().Interface(), fromField.Addr().Interface())
		if nil != err {
			copied = false
			if nil == accumulatedError {
				accumulatedError = err
				return false, accumulatedError
			}
			accumulatedError = fmt.Errorf("error copying %v\n%v", err, accumulatedError)
		} else {
			copied = true
		}
	}
	return copied, accumulatedError
}

func deepFields(ifaceType reflect.Type) []string {
	fields := []string{}

	if ifaceType.Kind() == reflect.Ptr && ifaceType.Elem().Kind() != reflect.Slice {
		// find all methods which take ptr as receiver
		fields = append(fields, deepFields(ifaceType.Elem())...)
	}

	// repeat (or do it for the first time) for all by-value-receiver methods
	fields = append(fields, deepFieldsImpl(ifaceType)...)
	return fields
}

func deepFieldsImpl(ifaceType reflect.Type) []string {
	fields := []string{}

	if ifaceType.Kind() != reflect.Ptr && ifaceType.Kind() != reflect.Struct || ifaceType.Elem().Kind() == reflect.Slice {
		return fields
	}

	methods := ifaceType.NumMethod()
	for i := 0; i < methods; i++ {
		var v reflect.Method
		v = ifaceType.Method(i)

		fields = append(fields, v.Name)
	}

	if ifaceType.Kind() == reflect.Ptr {
		return fields
	}

	elements := ifaceType.NumField()
	for i := 0; i < elements; i++ {
		var v reflect.StructField
		v = ifaceType.Field(i)

		fields = append(fields, v.Name)
	}

	return fields
}
