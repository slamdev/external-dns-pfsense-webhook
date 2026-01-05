package testdata

import (
	"encoding/json"
	"net"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
)

func RndName() string {
	// Generate a UUID and format it
	u := uuid.New().String()
	return sanitizeName(u)
}

func sanitizeName(name string) string {
	res := strings.ReplaceAll(strings.ToLower(name), "-", "")
	// Add prefix and ensure max length
	prefixed := "test" + res
	if len(prefixed) <= 25 {
		return prefixed
	}
	return prefixed[:25]
}

func CopyStruct[T any](src T) T {
	marshalled, err := json.Marshal(src)
	if err != nil {
		panic(err)
	}
	copied := new(T)
	err = json.Unmarshal(marshalled, &copied)
	if err != nil {
		panic(err)
	}
	return *copied
}

func EqualStructs(expected, actual any) bool {
	expectedJson, _ := json.Marshal(expected)
	actualJson, _ := json.Marshal(actual)

	var expectedMap map[string]any
	_ = json.Unmarshal(expectedJson, &expectedMap)

	var actualMap map[string]any
	_ = json.Unmarshal(actualJson, &actualMap)

	return cmp.Equal(expectedMap, actualMap)
}

func GetFreePort() (port int) {
	var a *net.TCPAddr
	var err error
	if a, err = net.ResolveTCPAddr("tcp", "localhost:0"); err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			defer l.Close()
			return l.Addr().(*net.TCPAddr).Port
		}
	}
	panic(err)
}
