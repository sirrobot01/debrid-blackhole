package utils

func RemoveItem[S ~[]E, E comparable](s S, values ...E) S {
	result := make(S, 0, len(s))
outer:
	for _, item := range s {
		for _, v := range values {
			if item == v {
				continue outer
			}
		}
		result = append(result, item)
	}
	return result
}
