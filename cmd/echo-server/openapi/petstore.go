package openapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"

	"github.com/gorilla/mux"
)

// Pet represents a pet in the store
type Pet struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Tag  string `json:"tag,omitempty"`
}

// Error represents an error response
type Error struct {
	Code    int32  `json:"code"`
	Message string `json:"message"`
}

// PetStore manages the pets collection
type PetStore struct {
	mu     sync.RWMutex
	pets   map[int64]*Pet
	nextID int64
}

// NewPetStore creates a new PetStore instance
func NewPetStore() *PetStore {
	store := &PetStore{
		pets:   make(map[int64]*Pet),
		nextID: 1,
	}
	// Add some sample pets
	store.pets[1] = &Pet{ID: 1, Name: "Fluffy", Tag: "cat"}
	store.pets[2] = &Pet{ID: 2, Name: "Rex", Tag: "dog"}
	store.nextID = 3
	return store
}

// ListPets handles GET /pets
func (ps *PetStore) ListPets(w http.ResponseWriter, r *http.Request) {
	// ps.setCORSHeaders(w)
	w.Header().Set("Content-Type", "application/json")

	// Parse limit parameter
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			if parsedLimit > 100 {
				limit = 100
			} else {
				limit = parsedLimit
			}
		}
	}

	ps.mu.RLock()
	defer ps.mu.RUnlock()

	pets := make([]*Pet, 0, len(ps.pets))
	for _, pet := range ps.pets {
		if len(pets) >= limit {
			break
		}
		pets = append(pets, pet)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(pets)
}

// CreatePets handles POST /pets
func (ps *PetStore) CreatePets(w http.ResponseWriter, r *http.Request) {
	// ps.setCORSHeaders(w)
	w.Header().Set("Content-Type", "application/json")

	var pet Pet
	if err := json.NewDecoder(r.Body).Decode(&pet); err != nil {
		ps.sendError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if pet.Name == "" {
		ps.sendError(w, http.StatusBadRequest, "Pet name is required")
		return
	}

	ps.mu.Lock()
	pet.ID = ps.nextID
	ps.nextID++
	ps.pets[pet.ID] = &pet
	ps.mu.Unlock()

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(pet)
}

// ShowPetById handles GET /pets/{petId}
func (ps *PetStore) ShowPetById(w http.ResponseWriter, r *http.Request) {
	// ps.setCORSHeaders(w)
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	petID, err := strconv.ParseInt(vars["petId"], 10, 64)
	if err != nil {
		ps.sendError(w, http.StatusBadRequest, "Invalid pet ID")
		return
	}

	ps.mu.RLock()
	pet, exists := ps.pets[petID]
	ps.mu.RUnlock()

	if !exists {
		ps.sendError(w, http.StatusNotFound, "Pet not found")
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(pet)
}

// sendError sends an error response
func (ps *PetStore) sendError(w http.ResponseWriter, code int, message string) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(Error{
		Code:    int32(code),
		Message: message,
	})
}
