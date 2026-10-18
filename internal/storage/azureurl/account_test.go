package azureurl

import "testing"

func TestEndpointsAcceptsOnlyCanonicalAccountOrigins(t *testing.T) {
	table, blob, err := Endpoints("https://account123.blob.core.windows.net")
	if err != nil || table != "https://account123.table.core.windows.net" || blob != "https://account123.blob.core.windows.net" {
		t.Fatalf("Endpoints(valid) = %q, %q, %v", table, blob, err)
	}
	invalid := []string{
		"http://account.blob.core.windows.net",
		"https://Account.blob.core.windows.net",
		"https://ab.blob.core.windows.net",
		"https://abcdefghijklmnopqrstuvwxy.blob.core.windows.net",
		"https://account-name.blob.core.windows.net",
		"https://account.blob.core.windows.net.",
		"https://account.blob.core.windows.net/",
		"https://account.blob.core.windows.net:443",
		"https://user@account.blob.core.windows.net",
		"https://account.blob.core.windows.net?",
		"https://account.blob.core.windows.net?x=1",
		"https://account.blob.core.windows.net#fragment",
		"https://account%2e.blob.core.windows.net",
	}
	for _, value := range invalid {
		t.Run(value, func(t *testing.T) {
			if _, _, err := Endpoints(value); err == nil {
				t.Fatalf("Endpoints(%q) accepted a non-canonical URL", value)
			}
		})
	}
}
