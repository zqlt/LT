















package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)













type URL struct {
	Scheme string 
	Path   string 
}


func parseURL(url string) (URL, error) {
	parts := strings.Split(url, ":
	if len(parts) != 2 || parts[0] == "" {
		return URL{}, errors.New("protocol scheme missing")
	}
	return URL{
		Scheme: parts[0],
		Path:   parts[1],
	}, nil
}


func (u URL) String() string {
	if u.Scheme != "" {
		return fmt.Sprintf("%s:
	}
	return u.Path
}


func (u URL) TerminalString() string {
	url := u.String()
	if len(url) > 32 {
		return url[:31] + "…"
	}
	return url
}


func (u URL) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.String())
}


func (u *URL) UnmarshalJSON(input []byte) error {
	var textURL string
	err := json.Unmarshal(input, &textURL)
	if err != nil {
		return err
	}
	url, err := parseURL(textURL)
	if err != nil {
		return err
	}
	u.Scheme = url.Scheme
	u.Path = url.Path
	return nil
}







func (u URL) Cmp(url URL) int {
	if u.Scheme == url.Scheme {
		return strings.Compare(u.Path, url.Path)
	}
	return strings.Compare(u.Scheme, url.Scheme)
}
