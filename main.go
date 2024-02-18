package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/mux"
)

type Store interface {
	Add(shortenedURL, longURL string) error
	Remove(shortenedURL string) error
	Get(shortenedURL string) (string, error)
}

type MemoryStore struct {
	items map[string]string
}

func (m *MemoryStore) Add(shortenedURL, longURL string) error {
	if m.items[shortenedURL] != "" {
		return fmt.Errorf("shortened URL already exists")
	}
	m.items[shortenedURL] = longURL
	log.Println(m.items)
	return nil
}

func (m *MemoryStore) Remove(shortenedURL string) error {
	if m.items[shortenedURL] == "" {
		return fmt.Errorf("shortened URL does not exist")
	}
	delete(m.items, shortenedURL)
	log.Println(m.items)
	return nil
}

func (m *MemoryStore) Get(shortenedURL string) (string, error) {
	longURL, ok := m.items[shortenedURL]
	if !ok {
		return "", fmt.Errorf("shortened URL does not exist")
	}
	return longURL, nil
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		items: make(map[string]string),
	}
}

type AddPath struct {
	domain string
	store  Store
}

func (a *AddPath) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	type addPathRequest struct {
		URL string `json:"url"`
	}

	var parsed addPathRequest
	err := json.NewDecoder(r.Body).Decode(&parsed)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("unexpected error: %v", err)))
		return
	}

	h := sha1.New()
	h.Write([]byte(parsed.URL))
	sum := h.Sum(nil)
	hash := hex.EncodeToString(sum)[:10]

	err = a.store.Add(hash, parsed.URL)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("unexpected error: %v", err)))
		return
	}

	type addPathResponse struct {
		ShortenedURL string `json:"shortened_url"`
		LongURL      string `json:"long_url"`
	}
	pathResp := addPathResponse{
		ShortenedURL: fmt.Sprintf("%v/%v", a.domain, hash),
		LongURL:      parsed.URL,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(pathResp)
	fmt.Fprintf(w, "%s", hash)
}

type DeletePath struct {
	store Store
}

func (p *DeletePath) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hash := mux.Vars(r)["hash"]

	if hash == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("shortened URL is empty"))
		return
	}

	err := p.store.Remove(hash)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("unexpected error: %v", err)))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("deleted"))
}

type RedirectPath struct {
	store Store
}

func (p *RedirectPath) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hash := mux.Vars(r)["hash"]
	if hash == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("shortened URL is empty"))
		return
	}
	longURL, err := p.store.Get(hash)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
		return
	}
	http.Redirect(w, r, longURL, http.StatusTemporaryRedirect)
}

// internal store
type internalStore struct {
	Version string            `json:"version"`
	Items   map[string]string `json:"items"`
}

type FileStore struct {
	filenane string
}

func (s *FileStore) Add(shortenedURL, longURL string) error {
	raw, err := os.ReadFile(s.filenane)
	if err != nil {
		return err
	}
	var is internalStore
	err = json.Unmarshal(raw, &is)
	if err != nil {
		return fmt.Errorf("unable to parse incoming JSON store data. Error: %v", err)
	}
	_, ok := is.Items[shortenedURL]
	if ok {
		return fmt.Errorf("shortened URL already exists")
	}
	is.Items[shortenedURL] = longURL
	modraw, err := json.Marshal(is)
	if err != nil {
		return fmt.Errorf("unable to generate JSON representation for file")
	}

	err = os.WriteFile(s.filenane, modraw, 0644)
	if err != nil {
		return err
	}
	return nil
}

func (s *FileStore) Remove(shortenedURL string) error {
	raw, err := os.ReadFile(s.filenane)
	if err != nil {
		return err
	}
	var is internalStore
	err = json.Unmarshal(raw, &is)
	if err != nil {
		return fmt.Errorf("unable to parse incoming JSON store data. Error: %v", err)
	}
	_, ok := is.Items[shortenedURL]
	if !ok {
		return fmt.Errorf("shortened URL does not exist")
	}
	delete(is.Items, shortenedURL)
	modraw, err := json.Marshal(is)
	if err != nil {
		return fmt.Errorf("unable to generate JSON representation for file")
	}
	err = os.WriteFile(s.filenane, modraw, 0644)
	if err != nil {
		return err
	}
	return nil
}

func (s *FileStore) Get(shortenedURL string) (string, error) {
	raw, err := os.ReadFile(s.filenane)
	if err != nil {
		return "", err
	}
	var is internalStore
	err = json.Unmarshal(raw, &is)
	if err != nil {
		return "", fmt.Errorf("unable to parse incoming JSON store data. Error: %v", err)
	}
	longURL, ok := is.Items[shortenedURL]
	if !ok {
		return "", fmt.Errorf("shortened URL does not exist")
	}
	return longURL, nil
}

func NewFileStore(filename string) (FileStore, error) {
	_, err := os.Stat(filename)
	if os.IsNotExist(err) {
		is := internalStore{Version: "1.0", Items: make(map[string]string)}
		raw, err := json.Marshal(is)
		if err != nil {
			return FileStore{}, fmt.Errorf("unable to generate JSON representation for file")
		}

		err = os.WriteFile(filename, raw, 0644)
		if err != nil {
			return FileStore{}, fmt.Errorf("unable to write to file")
		}
	}
	return FileStore{filenane: filename}, nil
}

func main() {
	log.Print("Hello world started")
	r := mux.NewRouter()
	fs, err := NewFileStore("store.json")
	if err != nil {
		panic("unable to create file store")
	}
	r.Handle("/add", &AddPath{domain: "http://localhost:8080", store: &fs}).Methods("POST")
	r.Handle("/{hash}", &DeletePath{store: &fs}).Methods("DELETE")
	r.Handle("/{hash}", &RedirectPath{store: &fs}).Methods("GET")
	http.ListenAndServe(":8080", r)
}
