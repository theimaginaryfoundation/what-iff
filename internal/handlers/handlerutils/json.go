package handlerutils

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
)

func DecodeJSON(r *http.Request, v interface{}) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println(err.Error())
		}
	}(r.Body)

	return json.Unmarshal(body, v)
}
