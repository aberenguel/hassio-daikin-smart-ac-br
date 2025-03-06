package config

import (
	"fmt"
	"strings"
)

const zeroconfAPNPrefix = "DAIKIN:"
const thingAPNPrefix = "DAIKIN"

// convertZeroConfAPN converts a Zeroconf APN (Access Point Name) to a Thing APN format.
// The function removes the prefix, reverses the remaining hexadecimal string,
// and uses the last 6 characters to a predefined Thing APN prefix.
//
// Parameters:
// zeroconfAPN (string): The input Zeroconf APN to be converted. Exemple: "DAIKIN:A07B79AB5497"
//
// Returns:
// string: The converted Thing APN. Exemple: "DAIKIN797BA0"
func convertZeroConfAPN(zeroconfAPN string) string {

	if strings.HasPrefix(zeroconfAPN, zeroconfAPNPrefix) {
		zeroconfAPNHex, _ := strings.CutPrefix(zeroconfAPN, zeroconfAPNPrefix)
		apnHex, err := reverseHexString(zeroconfAPNHex)
		if err != nil {
			return zeroconfAPNHex
		}
		return thingAPNPrefix + apnHex[len(apnHex)-6:]
	}
	return zeroconfAPN
}

func reverseHexString(hexStr string) (string, error) {

	// Remover ":" ou outros separadores comuns, caso existam
	hexStr = strings.ReplaceAll(hexStr, ":", "")

	// Verificar se a string tem um número par de caracteres
	if len(hexStr)%2 != 0 {
		return "", fmt.Errorf("not a valid hex string")
	}

	ret := ""
	for i := len(hexStr); i > 0; i -= 2 {
		ret += hexStr[i-2 : i]
	}
	return ret, nil
}
