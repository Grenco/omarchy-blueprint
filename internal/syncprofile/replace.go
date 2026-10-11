package syncprofile

// Replace selects only one provider-owned identity; inputs must be cloned by
// the caller before passing reference-bearing values.
func Replace[T any](dst, src []T, key string, identity func(T) string) []T {
	out := make([]T, 0, len(dst)+1)
	for _, x := range dst {
		if identity(x) != key {
			out = append(out, x)
		}
	}
	for _, x := range src {
		if identity(x) == key {
			out = append(out, x)
		}
	}
	return out
}
