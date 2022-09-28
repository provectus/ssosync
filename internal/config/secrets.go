package config

import (
	"context"
	"encoding/base64"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// Secrets ...
type Secrets struct {
	svc *secretsmanager.Client
}

// NewSecrets ...
func NewSecrets(svc *secretsmanager.Client) *Secrets {
	return &Secrets{
		svc: svc,
	}
}

// GoogleAdminEmail ...
func (s *Secrets) GoogleAdminEmail() (string, error) {
	return s.getSecret("SSOSyncGoogleAdminEmail")
}

// GoogleCredentials ...
func (s *Secrets) GoogleCredentials() (string, error) {
	return s.getSecret("SSOSyncGoogleCredentials")
}

func (s *Secrets) getSecret(secretKey string) (string, error) {
	r, err := s.svc.GetSecretValue(
		context.TODO(),
		&secretsmanager.GetSecretValueInput{
			SecretId:     aws.String(secretKey),
			VersionStage: aws.String("AWSCURRENT"),
		})

	if err != nil {
		return "", err
	}

	var secretString string

	if r.SecretString != nil {
		secretString = *r.SecretString
	} else {
		decodedBinarySecretBytes := make([]byte, base64.StdEncoding.DecodedLen(len(r.SecretBinary)))
		l, err := base64.StdEncoding.Decode(decodedBinarySecretBytes, r.SecretBinary)
		if err != nil {
			return "", err
		}
		secretString = string(decodedBinarySecretBytes[:l])
	}

	return secretString, nil
}
