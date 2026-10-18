package azureurl

import (
	"errors"
	"net/url"
	"regexp"
)

const blobSuffix = ".blob.core.windows.net"

var accountName = regexp.MustCompile(`^[a-z0-9]{3,24}$`)

// Endpoints accepts only the exact canonical public Azure Blob service origin.
func Endpoints(raw string) (tableURL, blobURL string, err error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Opaque != "" || parsed.User != nil || parsed.Port() != "" || parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" {
		return "", "", errors.New("invalid Azure Storage account URL")
	}
	host := parsed.Hostname()
	if len(host) <= len(blobSuffix) || host[len(host)-len(blobSuffix):] != blobSuffix {
		return "", "", errors.New("azure storage account URL must be the canonical Blob endpoint")
	}
	account := host[:len(host)-len(blobSuffix)]
	if !accountName.MatchString(account) || parsed.Host != account+blobSuffix || parsed.String() != raw {
		return "", "", errors.New("azure storage account URL must be the canonical Blob endpoint")
	}
	return "https://" + account + ".table.core.windows.net", "https://" + account + blobSuffix, nil
}
