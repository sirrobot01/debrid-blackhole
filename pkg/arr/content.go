package arr

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type ContentRequest struct {
	ID    string `json:"id"`
	Title string `json:"name"`
	Arr   string `json:"arr"`
}

func (a *Arr) GetContents() *ContentRequest {
	resp, err := a.Request(http.MethodGet, "api/v3/series", nil)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var data *ContentRequest
	if err = json.NewDecoder(resp.Body).Decode(&data); err != nil {
		fmt.Printf("Error: %v\n", err)
		return nil
	}
	fmt.Printf("Data: %v\n", data)
	//data.Arr = a.Name
	return data
}
