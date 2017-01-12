package copier

import (
	"fmt"

	log "github.com/Sirupsen/logrus"
	"reflect"
	"strings"
)

func Copy(toValue interface{}, fromValue interface{}) (err error) {
	from := reflect.ValueOf(fromValue)
	to := reflect.ValueOf(toValue)

	return copyImpl(to, from)
}

func copyImpl(to reflect.Value, from reflect.Value) error {
	log.Errorf("======begin copyImpl")
	defer log.Errorf("======end copyImpl")
	var (
		isSlice  bool
		isStruct bool
	)
	var accumulatedError error

	toReducted := reductPointers(to)
	fromReducted := reductPointers(from)

	toKind := to.Kind()
	fromKind := from.Kind()

	toType := deepType(toReducted.Type())
	fromType := deepType(fromReducted.Type())

	toExact := exactValue(toReducted)
	fromExact := exactValue(fromReducted)

	toDepth := depth(to.Type())
	fromDepth := depth(from.Type())

	// different levels of indirection. not an error, though
	if toDepth != fromDepth {
		return nil
	}

	if toKind == reflect.Slice {
		isSlice = true
	}

	if toKind == reflect.Struct || toReducted.IsValid() && toReducted.Kind() == reflect.Ptr && toReducted.Elem().IsValid() && toReducted.Type().Elem().Kind() == reflect.Struct {
		isStruct = true
	}

	// destination is a slice
	if isSlice {
		// length without any indirection
		amount := deepLen(from)

		err := copySliceImpl(toExact, toKind, toType, fromExact, fromKind, fromType, amount, nil)
		if nil != err {
			return err
		}
		return nil
	}

	log.Infof("toReducted.Type() = %+v isStruct = %t", toReducted.Type(), isStruct)
	// destination is a struct
	if isStruct {
		for _, name := range deepFields(toReducted.Type()) {
			log.Infof("NAME = %s", name)
			fromField := fieldByName(fromReducted, name)
			fromMethod := methodByName(fromReducted, name)
			log.Errorf("name = %+v fromField = %+v fromMethod = %+v", name, fromField, fromMethod)

			var from reflect.Value

			if fromField.IsValid() {
				from = fromField
			} else if fromMethod.IsValid() && fromMethod.Type().NumOut() == 1 {
				from = fromMethod.Call([]reflect.Value{})[0]
			} else {
				continue
			}

			toField := fieldByName(toReducted, name)
			log.Errorf("toField = %+v", toField)

			// if struct field is a slice we must create it here
			if toField.IsValid() && toField.Kind() == reflect.Slice && toField.IsNil() {
				capacity := 1
				if from.Kind() == reflect.Slice {
					capacity = from.Len()
				}
				// invoke the same method as for a root-level slice
				err := copySliceImpl(toField, toField.Kind(), toField.Type(), from, from.Kind(), from.Type(), capacity, nil)
				if nil != err {
					if nil == accumulatedError {
						accumulatedError = err
					} else {
						accumulatedError = fmt.Errorf("%v\n%v", err, accumulatedError)
					}
				}

				continue
			}

			toMethod := methodByName(toReducted, name)

			// we can't make stuff like deep copies when copying to a method
			canCopy := from.IsValid() && toMethod.IsValid() && toMethod.Kind() == reflect.Func && toMethod.Type().NumIn() == 1 && from.Type().AssignableTo(toMethod.Type().In(0))
			if canCopy {
				toMethod.Call([]reflect.Value{from})
				continue
			}

			log.Infof("[1]")
			_, accumulatedError = copyValue(toField, from, accumulatedError)
		}
		return accumulatedError
	}

	var err error
	log.Infof("[2]")
	_, err = copyValue(to, from, accumulatedError)
	return err
}

func fieldByName(base reflect.Value, name string) reflect.Value {
	if !base.IsValid() {
		return reflect.Zero(base.Type())
	}

	if base.Kind() == reflect.Ptr && !base.IsNil() {
		return fieldByName(base.Elem(), name)
	}
	if base.Kind() == reflect.Struct {
		return base.FieldByName(name)
	}
	return reflect.Zero(base.Type())
}

func methodByName(base reflect.Value, name string) reflect.Value {
	if !base.IsValid() {
		return reflect.Zero(base.Type())
	}

	if base.Kind() == reflect.Ptr && !base.IsNil() {
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
	if !to.IsValid() {
		return false, fmt.Errorf("destination is invalid")
	}

	if !from.IsValid() {
		return false, fmt.Errorf("source is invalid")
	}

	log.Errorf("======copyValue")

	// this copy will work if and only if both values are primitive types
	err := tryCopyPrimitive(to, from)

	if err == nil {
		return true, nil
	}

	copied, accumulatedError := tryDeepCopyPtr(to, from, accumulatedError)
	if !copied {
		copied, accumulatedError = tryDeepCopyStruct(to, from, accumulatedError)
	}

	return copied, accumulatedError
}

func tryCopyPrimitive(dest reflect.Value, src reflect.Value) error {
	if !dest.IsValid() {
		return fmt.Errorf("destination is invalid")
	}

	if !src.IsValid() {
		return fmt.Errorf("source is invalid")
	}

	if !dest.CanSet() {
		return fmt.Errorf("destination is not settable")
	}

	if !src.Type().AssignableTo(dest.Type()) {
		return fmt.Errorf("destination type %v is not compatible with source type %v", dest.Type(), src.Type())
	}

	if dest.Kind() == reflect.Ptr {
		return fmt.Errorf("destination type %v is a pointer, pointers must not be assigned in this way", dest.Type())
	}

	log.Infof("PRIMITIVE")
	dest.Set(src)

	return nil
}

func tryDeepCopyPtr(toField reflect.Value, fromField reflect.Value, accumulatedError error) (bool, error) {
	toDepth := depth(toField.Type())
	fromDepth := depth(fromField.Type())

	deepCopyRequired := toField.Type().Kind() == reflect.Ptr && fromField.Type().Kind() == reflect.Ptr &&
		!fromField.IsNil() && (toField.CanSet() || !toField.IsNil()) && toDepth == fromDepth && toField.IsValid() && fromField.IsValid()

	copied := false
	if deepCopyRequired {
		fromField = reductPointers(fromField)

		for toField.IsValid() && toField.Kind() == reflect.Ptr && toField.IsNil() {
			if !toField.CanSet() {
				return false, fmt.Errorf("cannot set empty pointer")
			}

			newTo := reflect.New(toField.Type().Elem())
			toField.Set(newTo)
			toField = reductPointers(toField)
		}

		toExact := exactValue(toField)
		fromExact := exactValue(fromField)

		log.Infof("DEEP PTR")
		err := copyImpl(toExact, fromExact)

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
	if toField.Kind() == reflect.Ptr && toField.IsNil() {
	}
	if deepCopyRequired {
		log.Infof("DEEP STRUCT")
		err := copyImpl(toField, fromField)
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

func copySliceImpl(toExact reflect.Value, toKind reflect.Kind, toType reflect.Type,
	fromExact reflect.Value, fromKind reflect.Kind, fromType reflect.Type, amount int, accumulatedError error) error {

	if fromKind == reflect.Slice {
		if toExact.IsNil() {
			toExact.Set(reflect.MakeSlice(toType, 0, amount))
		}

		newSlice := reflect.MakeSlice(toType, amount, amount)
		originalLen := toExact.Len()
		toExact.Set(reflect.AppendSlice(toExact, newSlice))

		for i := 0; i < amount; i++ {
			var newT reflect.Value

			newT = reflect.Indirect(reflect.New(toType.Elem()))

			err := copyImpl(newT, fromExact.Index(i))
			toExact.Index(originalLen + i).Set(newT)

			if nil != err {
				if nil == accumulatedError {
					accumulatedError = err
					continue
				}
				accumulatedError = fmt.Errorf("error copying %v\n%v", err, accumulatedError)
			}
		}
	} else if fromType.AssignableTo(toType.Elem()) {
		return fmt.Errorf("copy from element to slice unsupported\n%v", accumulatedError)
	} else {
		return fmt.Errorf("source slice type unsupported\n%v", accumulatedError)
	}

	return nil
}

func deepFields(ifaceType reflect.Type) []string {
	log.Infof("deepFields = %v", ifaceType)
	fields := []string{}

	if ifaceType.Kind() == reflect.Ptr && ifaceType.Elem().Kind() == reflect.Struct {
		// find all methods which take ptr as receiver
		fields = append(fields, deepFields(ifaceType.Elem())...)
	}

	// repeat (or do it for the first time) for all by-value-receiver methods
	fields = append(fields, deepFieldsImpl(ifaceType)...)

	return fields
}

func deepFieldsImpl(ifaceType reflect.Type) []string {
	fields := []string{}

	if ifaceType.Kind() != reflect.Ptr && ifaceType.Kind() != reflect.Struct ||
		ifaceType.Kind() == reflect.Ptr && ifaceType.Elem().Kind() == reflect.Slice {
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

		if len(v.Name) == 0 || v.Name[0:1] != strings.ToUpper(v.Name[0:1]) {
			continue
		}

		fields = append(fields, v.Name)
	}

	return fields
}

func deepLen(array reflect.Value) int {
	if array.Kind() == reflect.Slice {
		return array.Len()
	} else if array.Kind() == reflect.Ptr {
		return deepLen(array.Elem())
	}

	return 1
}

func deepKind(ptr reflect.Type) reflect.Kind {
	if ptr.Kind() == reflect.Ptr {
		return deepKind(ptr.Elem())
	}

	return ptr.Kind()
}

func depth(ptr reflect.Type) int {
	if ptr.Kind() == reflect.Ptr {
		return 1 + depth(ptr.Elem())
	}

	return 0
}

func deepType(ptrType reflect.Type) reflect.Type {
	if ptrType.Kind() == reflect.Ptr {
		return deepType(ptrType.Elem())
	}

	return ptrType
}

func reductPointers(ptr reflect.Value) reflect.Value {
	if ptr.Kind() == reflect.Ptr && ptr.Elem().Kind() == reflect.Ptr {
		return reductPointers(ptr.Elem())
	}
	return ptr
}

func exactValue(ptr reflect.Value) reflect.Value {
	if ptr.Kind() == reflect.Ptr {
		return exactValue(ptr.Elem())
	}
	return ptr
}
