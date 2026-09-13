package main

import "errors"

type credentials struct {
	User     string
	Password string
}

func loadCredentials(getenv func(string) string) (credentials, error) {
	result := credentials{
		User:     getenv("WEBDAV_USER"),
		Password: getenv("WEBDAV_PASSWORD"),
	}
	if result.User == "" {
		return credentials{}, errors.New("WEBDAV_USER is required")
	}
	if result.Password == "" {
		return credentials{}, errors.New("WEBDAV_PASSWORD is required")
	}
	return result, nil
}
