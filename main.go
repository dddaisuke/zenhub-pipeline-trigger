package main

import (
  "encoding/gob"
  "errors"
  "flag"
  "fmt"
  "hash/fnv"
  "io/ioutil"
  "log"
  "net/http"
  "net/http/httptest"
  "net/url"
  "os"
  "os/exec"
  "path/filepath"
  "runtime"
  "strings"
  "time"

  "golang.org/x/net/context"
  "golang.org/x/oauth2"
  "golang.org/x/oauth2/google"

  tasks "google.golang.org/api/tasks/v1"
)


// Flags
var (
  clientID     = flag.String("clientid", "", "OAuth 2.0 Client ID.  If non-empty, overrides --clientid_file")
  clientIDFile = flag.String("clientid-file", "clientid.dat",
    "Name of a file containing just the project's OAuth 2.0 Client ID from https://developers.google.com/console.")
  secret     = flag.String("secret", "", "OAuth 2.0 Client Secret.  If non-empty, overrides --secret_file")
  secretFile = flag.String("secret-file", "clientsecret.dat",
    "Name of a file containing just the project's OAuth 2.0 Client Secret from https://developers.google.com/console.")
  cacheToken = flag.Bool("cachetoken", true, "cache the OAuth 2.0 token")
  debug      = flag.Bool("debug", false, "show HTTP traffic")
)


func main() {
  config := &oauth2.Config{
    ClientID:     "98171848281-pb32huc72ece36eqokckm6qds6v32npq.apps.googleusercontent.com", //valueOrFileContents(*clientID, *clientIDFile),
    ClientSecret: "WyxcD92qxESUtCmTfj71xDOC", //valueOrFileContents(*secret, *secretFile),
    Endpoint:     google.Endpoint,
    Scopes:       []string{tasks.TasksScope},
  }

  ctx := context.Background()
  c := newOAuthClient(ctx, config)
  tasksMain(c)
}

var (
  demoFunc  = make(map[string]func(*http.Client, []string))
  demoScope = make(map[string]string)
)

func registerDemo(name, scope string, main func(c *http.Client, argv []string)) {
  if demoFunc[name] != nil {
    panic(name + " already registered")
  }
  demoFunc[name] = main
  demoScope[name] = scope
}

func osUserCacheDir() string {
  switch runtime.GOOS {
  case "darwin":
    return filepath.Join(os.Getenv("HOME"), "Library", "Caches")
  case "linux", "freebsd":
    return filepath.Join(os.Getenv("HOME"), ".cache")
  }
  log.Printf("TODO: osUserCacheDir on GOOS %q", runtime.GOOS)
  return "."
}

func tokenCacheFile(config *oauth2.Config) string {
  hash := fnv.New32a()
  hash.Write([]byte(config.ClientID))
  hash.Write([]byte(config.ClientSecret))
  hash.Write([]byte(strings.Join(config.Scopes, " ")))
  fn := fmt.Sprintf("go-api-demo-tok%v", hash.Sum32())
  return filepath.Join(osUserCacheDir(), url.QueryEscape(fn))
}

func tokenFromFile(file string) (*oauth2.Token, error) {
  if !*cacheToken {
    return nil, errors.New("--cachetoken is false")
  }
  f, err := os.Open(file)
  if err != nil {
    return nil, err
  }
  t := new(oauth2.Token)
  err = gob.NewDecoder(f).Decode(t)
  return t, err
}

func saveToken(file string, token *oauth2.Token) {
  f, err := os.Create(file)
  if err != nil {
    log.Printf("Warning: failed to cache oauth token: %v", err)
    return
  }
  defer f.Close()
  gob.NewEncoder(f).Encode(token)
}

func newOAuthClient(ctx context.Context, config *oauth2.Config) *http.Client {
  cacheFile := tokenCacheFile(config)
  token, err := tokenFromFile(cacheFile)
  if err != nil {
    token = tokenFromWeb(ctx, config)
    saveToken(cacheFile, token)
  } else {
    log.Printf("Using cached token %#v from %q", token, cacheFile)
  }

  return config.Client(ctx, token)
}

func tokenFromWeb(ctx context.Context, config *oauth2.Config) *oauth2.Token {
  ch := make(chan string)
  randState := fmt.Sprintf("st%d", time.Now().UnixNano())
  ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
    if req.URL.Path == "/favicon.ico" {
      http.Error(rw, "", 404)
      return
    }
    if req.FormValue("state") != randState {
      log.Printf("State doesn't match: req = %#v", req)
      http.Error(rw, "", 500)
      return
    }
    if code := req.FormValue("code"); code != "" {
      fmt.Fprintf(rw, "<h1>Success</h1>Authorized.")
      rw.(http.Flusher).Flush()
      ch <- code
      return
    }
    log.Printf("no code")
    http.Error(rw, "", 500)
  }))
  defer ts.Close()

  config.RedirectURL = ts.URL
  authURL := config.AuthCodeURL(randState)
  go openURL(authURL)
  log.Printf("Authorize this app at: %s", authURL)
  code := <-ch
  log.Printf("Got code: %s", code)

  token, err := config.Exchange(ctx, code)
  if err != nil {
    log.Fatalf("Token exchange error: %v", err)
  }
  return token
}

func openURL(url string) {
  try := []string{"xdg-open", "google-chrome", "open"}
  for _, bin := range try {
    err := exec.Command(bin, url).Run()
    if err == nil {
      return
    }
  }
  log.Printf("Error opening URL in browser.")
}

func valueOrFileContents(value string, filename string) string {
  if value != "" {
    return value
  }
  slurp, err := ioutil.ReadFile(filename)
  if err != nil {
    log.Fatalf("Error reading %q: %v", filename, err)
  }
  return strings.TrimSpace(string(slurp))
}

func tasksMain(client *http.Client) {
  taskapi, err := tasks.New(client)
  if err != nil {
    log.Fatalf("Unable to create Tasks service: %v", err)
  }

  task, err := taskapi.Tasks.Insert("@default", &tasks.Task{
    Title: "finish this API code generator thing",
    Notes: "ummmm",
    Due:   "2011-10-15T12:00:00.000Z",
  }).Do()
  log.Printf("Got task, err: %#v, %v", task, err)
}
