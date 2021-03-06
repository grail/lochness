package main

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/justinas/alice"
	"github.com/mistifyio/lochness"
)

// RegisterGuestRoutes registers the guest routes and handlers
func RegisterGuestRoutes(prefix string, router *mux.Router, m *metricsContext) {
	guestMiddleware := alice.New(
		loadGuest,
	)

	router.Handle(prefix, m.mmw.HandlerFunc(ListGuests, "list")).Methods("GET")
	router.Handle(prefix, m.mmw.HandlerFunc(CreateGuest, "create")).Methods("POST")

	// TODO: Figure out a cleaner way to do middleware on the subrouter
	sub := router.PathPrefix(prefix).Subrouter()

	// XXX: could do a simple struct that had the info and range over it to set this up
	sub.Handle("/{guestID}", guestMiddleware.Append(m.mmw.HandlerWrapper("get")).ThenFunc(GetGuest)).Methods("GET")
	sub.Handle("/{guestID}", guestMiddleware.Append(m.mmw.HandlerWrapper("update")).ThenFunc(UpdateGuest)).Methods("PATCH")
	sub.Handle("/{guestID}", guestMiddleware.Append(m.mmw.HandlerWrapper("destroy")).ThenFunc(DestroyGuest)).Methods("DELETE")
	// Limit actions and have specific action metrics while sharing a handler
	for _, action := range []string{"shutdown", "reboot", "restart", "poweroff", "start", "suspend"} {
		sub.Handle(fmt.Sprintf("/{guestID}/{action:%s}", action),
			guestMiddleware.
				Append(m.mmw.HandlerWrapper(action)).
				ThenFunc(GuestAction),
		).Methods("POST")
	}
}

// ListGuests gets a list of all guests
func ListGuests(w http.ResponseWriter, r *http.Request) {
	hr := HTTPResponse{w}
	ctx := GetContext(r)
	guests := make(lochness.Guests, 0)
	err := ctx.ForEachGuest(func(g *lochness.Guest) error {
		guests = append(guests, g)
		return nil
	})
	if err != nil {
		hr.JSONError(http.StatusInternalServerError, err)
		return
	}
	hr.JSON(http.StatusOK, guests)
}

// CreateGuest creates a new guest
func CreateGuest(w http.ResponseWriter, r *http.Request) {
	hr := HTTPResponse{w}

	guest, err := decodeGuest(r, nil)
	if err != nil {
		hr.JSONMsg(http.StatusBadRequest, err.Error())
		return
	}

	// Hypervisor will be selected automatically
	guest.HypervisorID = ""

	if !saveGuestHelper(hr, guest) {
		return
	}

	guestNewJobHelper(hr, r, guest, "select-hypervisor")
}

// GetGuest gets a particular guest
func GetGuest(w http.ResponseWriter, r *http.Request) {
	hr := HTTPResponse{w}
	hr.JSON(http.StatusOK, GetRequestGuest(r))
}

// UpdateGuest updates an existing guest
func UpdateGuest(w http.ResponseWriter, r *http.Request) {
	hr := HTTPResponse{w}
	guest := GetRequestGuest(r)

	_, err := decodeGuest(r, guest)
	if err != nil {
		hr.JSONMsg(http.StatusBadRequest, err.Error())
		return
	}

	if !saveGuestHelper(hr, guest) {
		return
	}
	hr.JSON(http.StatusOK, guest)
}

// DestroyGuest removes a guest and frees its IP
func DestroyGuest(w http.ResponseWriter, r *http.Request) {
	hr := HTTPResponse{w}
	guest := GetRequestGuest(r)

	guestNewJobHelper(hr, r, guest, "delete")
}

// GuestAction handles all of the generic guest actions
func GuestAction(w http.ResponseWriter, r *http.Request) {
	hr := HTTPResponse{w}
	guest := GetRequestGuest(r)

	vars := mux.Vars(r)

	guestNewJobHelper(hr, r, guest, vars["action"])
}
