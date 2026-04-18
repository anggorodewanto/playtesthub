// Copyright (c) 2023-2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package common

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
)

func GetEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}

	return fallback
}

func GetEnvInt(key string, fallback int) int {
	str := GetEnv(key, strconv.Itoa(fallback))
	val, err := strconv.Atoi(str)
	if err != nil {
		return fallback
	}

	return val
}

func GetBasePath() string {
	basePath := os.Getenv("BASE_PATH")
	if basePath == "" {
		slog.Error("BASE_PATH envar is not set or empty")
		os.Exit(1)
	}
	if !strings.HasPrefix(basePath, "/") {
		slog.Error("BASE_PATH envar is invalid, no leading '/' found. Valid example: /basePath")
		os.Exit(1)
	}

	return basePath
}
