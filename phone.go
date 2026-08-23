package sendafrica

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var tzPhonePattern = regexp.MustCompile(`^\+255[67][0-9]{8}$`)

// NormalizeTZPhone converts accepted Tanzania mobile formats to E.164.
func NormalizeTZPhone(phone string) (string, error) {
	clean := strings.TrimSpace(phone)
	clean = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(clean)
	switch {
	case strings.HasPrefix(clean, "+255"):
		// Already E.164.
	case strings.HasPrefix(clean, "255"):
		clean = "+" + clean
	case strings.HasPrefix(clean, "0"):
		clean = "+255" + strings.TrimPrefix(clean, "0")
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidPhone, phone)
	}
	if !tzPhonePattern.MatchString(clean) {
		return "", fmt.Errorf("%w: %q", ErrInvalidPhone, phone)
	}
	return clean, nil
}

func IsValidTZPhone(phone string) bool { _, err := NormalizeTZPhone(phone); return err == nil }

type SMSEncoding string

const (
	EncodingGSM7 SMSEncoding = "GSM-7"
	EncodingUCS2 SMSEncoding = "UCS-2"
)

type SMSPartInfo struct {
	Encoding        SMSEncoding `json:"encoding"`
	Length          int         `json:"length"`
	Parts           int         `json:"parts"`
	CreditsRequired int         `json:"credits_required"`
}

// GSM-7 basic alphabet plus the extension table. Characters represented by the
// extension table consume two septets, but still remain GSM-7 encoded.
const gsm7Basic = "@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞ\u001bÆæßÉ !\"#¤%&'()*+,-./0123456789:;<=>?¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà"
const gsm7Extended = "^{}\\[~]|€"

func DetectEncoding(message string) SMSEncoding {
	for _, r := range message {
		if !strings.ContainsRune(gsm7Basic, r) && !strings.ContainsRune(gsm7Extended, r) {
			return EncodingUCS2
		}
	}
	return EncodingGSM7
}

func GetSMSPartInfo(message string) SMSPartInfo {
	encoding := DetectEncoding(message)
	length := utf8.RuneCountInString(message)
	if encoding == EncodingGSM7 {
		length = 0
		for _, r := range message {
			if strings.ContainsRune(gsm7Extended, r) {
				length += 2
			} else {
				length++
			}
		}
	}
	onePart, multiPart := 160, 153
	if encoding == EncodingUCS2 {
		onePart, multiPart = 70, 67
	}
	parts := 0
	if length > 0 {
		if length <= onePart {
			parts = 1
		} else {
			parts = (length + multiPart - 1) / multiPart
		}
	}
	return SMSPartInfo{Encoding: encoding, Length: length, Parts: parts, CreditsRequired: parts}
}
