package playa

import "fmt"

const (
	statusOK       = 1
	statusError    = 2
	statusNotFound = 404
)

type Status struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

type Rsp struct {
	Status Status `json:"status"`
}

type RspData[T any] struct {
	Status Status `json:"status"`
	Data   *T     `json:"data,omitempty"`
}

func okRsp[T any](data T) RspData[T] {
	return RspData[T]{Status: Status{Code: statusOK}, Data: &data}
}

func errorRsp(message string) Rsp {
	return Rsp{Status: Status{Code: statusError, Message: message}}
}

func notFoundRsp(kind string, id string) Rsp {
	return Rsp{Status: Status{Code: statusNotFound, Message: fmt.Sprintf("%s '%s' not found", kind, id)}}
}
