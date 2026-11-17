package app

const (
	CredentialDapodikCookie = "dapodik.cookie"
	CredentialDapodikToken  = "dapodik.token"
)

type CredentialState struct {
	Configured bool
	UpdatedAt  string
	Err        error
}
