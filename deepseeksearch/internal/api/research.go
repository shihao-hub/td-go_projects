package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"deepseeksearch/internal/research"
)

func (s *Server) researchRoutes(mux *http.ServeMux) {
	service := research.New(s.store)
	respond := func(w http.ResponseWriter, data interface{}, err error) {
		out := research.Wrap(data, err)
		status := http.StatusOK
		if out.Error != nil {
			switch out.Error.Code {
			case "invalid_argument":
				status = 400
			case "not_found":
				status = 404
			case "conflict":
				status = 409
			case "partial_failure":
				status = 207
			default:
				status = 502
			}
		}
		writeJSON(w, status, out)
	}
	decode := func(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
		if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
			respond(w, nil, &research.Error{Code: "invalid_argument", Message: "请求必须使用 application/json"})
			return false
		}
		r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		err := dec.Decode(dst)
		if err == nil {
			var extra interface{}
			if dec.Decode(&extra) != io.EOF {
				err = io.ErrUnexpectedEOF
			}
		}
		if err != nil {
			respond(w, nil, &research.Error{Code: "invalid_argument", Message: "请求 JSON 无效或包含未知字段"})
			return false
		}
		return true
	}
	mux.HandleFunc("GET /api/research", func(w http.ResponseWriter, r *http.Request) { v, e := service.List(r.Context()); respond(w, v, e) })
	mux.HandleFunc("POST /api/research", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Question string `json:"question"`
		}
		if !decode(w, r, &req) {
			return
		}
		v, e := service.Create(r.Context(), req.Question)
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/research/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := service.Get(r.Context(), r.PathValue("id"))
		respond(w, v, e)
	})
	mux.HandleFunc("PUT /api/research/{id}/draft", func(w http.ResponseWriter, r *http.Request) {
		var d research.Draft
		if !decode(w, r, &d) {
			return
		}
		v, e := service.SetDraft(r.Context(), r.PathValue("id"), d)
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/research/{id}/suggest", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
			respond(w, nil, &research.Error{Code: "invalid_argument", Message: "请求必须使用 application/json"})
			return
		}
		v, e := service.Suggest(r.Context(), r.PathValue("id"))
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/research/{id}/feedback", func(w http.ResponseWriter, r *http.Request) {
		var f research.Feedback
		if !decode(w, r, &f) {
			return
		}
		v, e := service.AddFeedback(r.Context(), r.PathValue("id"), f)
		respond(w, v, e)
	})
	mux.HandleFunc("POST /api/research/{id}/run", func(w http.ResponseWriter, r *http.Request) {
		var opt research.RunOptions
		if !decode(w, r, &opt) {
			return
		}
		v, e := service.Run(r.Context(), r.PathValue("id"), opt, nil)
		respond(w, v, e)
	})
}
