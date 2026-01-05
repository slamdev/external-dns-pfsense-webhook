package integration

func ToPointer[Value any](v Value) *Value {
	return &v
}

func ToSliceOfPointers[T any](vv ...T) []*T {
	slc := make([]*T, len(vv))
	for i := range vv {
		slc[i] = ToPointer(vv[i])
	}
	return slc
}

func FromPtr[T any](ptr *T, defaultValue ...T) T {
	if ptr != nil {
		return *ptr
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	var zero T
	return zero // Return zero value of T (e.g., 0 for int, "" for string)
}

func GetOrDefault[T comparable](ptr *T, defaultValue T) T {
	if ptr != nil {
		return *ptr
	}
	return defaultValue
}
