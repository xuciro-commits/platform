package protocol

import "time"

type EmailCodeRequest struct {
	Email string `json:"email"`
}

type EmailCodeAnswer struct {
	ExpiresInSeconds int `json:"expiresInSeconds"`
}

type EmailVerifyRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type AuthWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type AuthSession struct {
	Token     string          `json:"token,omitempty"`
	AccountID string          `json:"accountId"`
	Email     string          `json:"email"`
	Subject   string          `json:"subject"`
	Tenants   []AuthWorkspace `json:"tenants"`
}

type CapturedMail struct {
	ID      string    `json:"id"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	Subject string    `json:"subject"`
	Text    string    `json:"text"`
	SentAt  time.Time `json:"sentAt"`
}

type MailChannel struct {
	URL          string `json:"url"`
	From         string `json:"from"`
	Secret       string `json:"secret,omitempty"`
	AllowPrivate bool   `json:"allowPrivate,omitempty"`
}
