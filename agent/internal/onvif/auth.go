package onvif

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"time"
)

const (
	wsseNamespace = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"
	wsuNamespace  = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"
	passwordType  = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest"
	nonceEncoding = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary"
)

// securityHeader builds a WS-Security UsernameToken header using the digest
// form. created is the timestamp the camera's replay protection compares
// against; the caller supplies the camera clock, or the appliance clock when
// the unauthenticated read is unavailable.
func securityHeader(credentials Credentials, created time.Time) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate a security nonce: %w", err)
	}
	createdText := created.UTC().Format("2006-01-02T15:04:05.000Z")
	digestInput := make([]byte, 0, len(nonce)+len(createdText)+len(credentials.Password))
	digestInput = append(digestInput, nonce...)
	digestInput = append(digestInput, createdText...)
	digestInput = append(digestInput, credentials.Password...)
	sum := sha1.Sum(digestInput)
	passwordDigest := base64.StdEncoding.EncodeToString(sum[:])
	nonceText := base64.StdEncoding.EncodeToString(nonce)

	var buffer bytes.Buffer
	buffer.WriteString(`<wsse:Security xmlns:wsse="` + wsseNamespace + `" xmlns:wsu="` + wsuNamespace + `" s:mustUnderstand="1">`)
	buffer.WriteString(`<wsse:UsernameToken>`)
	buffer.WriteString(`<wsse:Username>`)
	writeXMLText(&buffer, credentials.Username)
	buffer.WriteString(`</wsse:Username>`)
	buffer.WriteString(`<wsse:Password Type="` + passwordType + `">`)
	buffer.WriteString(passwordDigest)
	buffer.WriteString(`</wsse:Password>`)
	buffer.WriteString(`<wsse:Nonce EncodingType="` + nonceEncoding + `">`)
	buffer.WriteString(nonceText)
	buffer.WriteString(`</wsse:Nonce>`)
	buffer.WriteString(`<wsu:Created>` + createdText + `</wsu:Created>`)
	buffer.WriteString(`</wsse:UsernameToken>`)
	buffer.WriteString(`</wsse:Security>`)
	return buffer.String(), nil
}

func writeXMLText(buffer *bytes.Buffer, value string) {
	_ = xml.EscapeText(buffer, []byte(value))
}
