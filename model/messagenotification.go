package model

type MessageNotification struct {
	Action string `json:"action"`
	Message *MessageExternal `json:"message"`
}