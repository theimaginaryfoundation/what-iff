package handlerutils

func StringOrEmpty(v *string) string {
	if v == nil {
		return ""
	}

	return *v
}
