package kiro

import (
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"os/user"

	"github.com/google/uuid"
)

// machineFingerprint generates a unique fingerprint based on hostname + username.
func machineFingerprint() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	username := "unknown"
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
	h := sha256.Sum256([]byte(fmt.Sprintf("%s-%s-kiro-gateway", hostname, username)))
	return fmt.Sprintf("%x", h)
}

// kiroHeaders builds the headers required for Kiro API requests.
func kiroHeaders(token, fingerprint string) map[string]string {
	return map[string]string{
		"Authorization":                    "Bearer " + token,
		"Content-Type":                     "application/json",
		"User-Agent":                       fmt.Sprintf("aws-sdk-js/1.0.27 ua/2.1 os/win32#10.0.19044 lang/js md/nodejs#22.21.1 api/codewhispererstreaming#1.0.27 m/E KiroIDE-0.7.45-%s", fingerprint),
		"x-amz-user-agent":                fmt.Sprintf("aws-sdk-js/1.0.27 KiroIDE-0.7.45-%s", fingerprint),
		"x-amzn-codewhisperer-optout":     "true",
		"x-amzn-kiro-agent-mode":          "vibe",
		"amz-sdk-invocation-id":           uuid.New().String(),
		"amz-sdk-request":                 "attempt=1; max=3",
	}
}

// localIP returns the machine's preferred outbound IP (for fingerprint fallback).
func localIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}
