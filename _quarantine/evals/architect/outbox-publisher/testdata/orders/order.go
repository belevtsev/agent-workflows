package orders

import "encoding/json"

type Order struct {
	ID    string `json:"id"`
	Total int64  `json:"total"`
}

func (o Order) Encode() []byte {
	data, _ := json.Marshal(o)
	return data
}
