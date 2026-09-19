package integration_test

import (
	"errors"
	"fmt"
	"github.com/kardolus/chatgpt-cli/config"
	"github.com/kardolus/chatgpt-cli/test"
	"github.com/onsi/gomega/gexec"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

const expectedToken = "valid-api-key"

var (
	onceBuild     sync.Once
	onceServe     sync.Once
	serverReady   = make(chan struct{})
	binaryPath    string
	mockServerURL string
)

func buildBinary(serviceURL string) error {
	var err error
	onceBuild.Do(func() {
		binaryPath, err = gexec.Build(
			"github.com/kardolus/chatgpt-cli/cmd/chatgpt",
			"-ldflags",
			fmt.Sprintf("-X main.GitCommit=%s -X main.GitVersion=%s -X main.ServiceURL=%s", gitCommit, gitVersion, serviceURL))
	})
	return err
}

func curl(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// runMockServer starts the mock API on an OS-assigned port and returns its base
// URL. The listener is created synchronously so that a bind failure surfaces as
// an error rather than being swallowed inside a goroutine.
//
// The port is not hardcoded because on Windows a second listener can bind a port
// another process already holds (neither sets SO_EXCLUSIVEADDRUSE); the two then
// race for incoming connections and requests fail intermittently with
// "An existing connection was forcibly closed by the remote host".
func runMockServer() (string, error) {
	var err error

	onceServe.Do(func() {
		defaults := config.NewStore().ReadDefaults()

		var listener net.Listener
		listener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			close(serverReady)
			return
		}
		mockServerURL = "http://" + listener.Addr().String()

		mux := http.NewServeMux()
		mux.HandleFunc("/ping", getPing)
		mux.HandleFunc(defaults.CompletionsPath, postCompletions)
		mux.HandleFunc(defaults.ResponsesPath, postResponses)
		mux.HandleFunc(defaults.ModelsPath, getModels)

		go func() {
			_ = http.Serve(listener, mux)
		}()
		close(serverReady)
	})

	<-serverReady
	return mockServerURL, err
}

func getPing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	_, _ = w.Write([]byte("pong"))
}

func getModels(w http.ResponseWriter, r *http.Request) {
	if err := validateRequest(w, r, http.MethodGet); err != nil {
		fmt.Printf("invalid request: %s\n", err.Error())
		return
	}

	if err := checkBearerToken(r, expectedToken); err != nil {
		http.Error(w, creatAuthError(), http.StatusUnauthorized)
		return
	}

	const modelFile = "models.json"
	response, err := test.FileToBytes(modelFile)
	if err != nil {
		fmt.Printf("error reading %s: %s\n", modelFile, err.Error())
		return
	}
	_, _ = w.Write(response)
}

func postCompletions(w http.ResponseWriter, r *http.Request) {
	if err := validateRequest(w, r, http.MethodPost); err != nil {
		fmt.Printf("invalid request: %s\n", err.Error())
		return
	}

	if err := checkBearerToken(r, expectedToken); err != nil {
		http.Error(w, creatAuthError(), http.StatusUnauthorized)
		return
	}

	const completionsFile = "completions.json"
	response, err := test.FileToBytes(completionsFile)
	if err != nil {
		fmt.Printf("error reading %s: %s\n", completionsFile, err.Error())
		return
	}
	_, _ = w.Write(response)
}

func postResponses(w http.ResponseWriter, r *http.Request) {
	if err := validateRequest(w, r, http.MethodPost); err != nil {
		fmt.Printf("invalid request: %s\n", err.Error())
		return
	}

	if err := checkBearerToken(r, expectedToken); err != nil {
		http.Error(w, creatAuthError(), http.StatusUnauthorized)
		return
	}

	const responsesFile = "responses.json"
	response, err := test.FileToBytes(responsesFile)
	if err != nil {
		fmt.Printf("error reading %s: %s\n", responsesFile, err.Error())
		return
	}
	_, _ = w.Write(response)
}

func checkBearerToken(r *http.Request, expectedToken string) error {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return errors.New("missing Authorization header")
	}

	splitToken := strings.Split(authHeader, "Bearer ")
	if len(splitToken) != 2 {
		return errors.New("malformed Authorization header")
	}

	requestToken := splitToken[1]
	if requestToken != expectedToken {
		return errors.New("invalid token")
	}

	return nil
}

func creatAuthError() string {
	const errorFile = "error.json"

	response, err := test.FileToBytes(errorFile)
	if err != nil {
		fmt.Printf("error reading %s: %s\n", errorFile, err.Error())
		return ""
	}

	return string(response)
}

func validateRequest(w http.ResponseWriter, r *http.Request, allowedMethod string) error {
	if r.Method != allowedMethod {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return errors.New("method not allowed")
	}

	if !strings.Contains(r.Header.Get("Authorization"), "Bearer") {
		w.WriteHeader(http.StatusBadRequest)
		return errors.New("bad request")
	}

	return nil
}
