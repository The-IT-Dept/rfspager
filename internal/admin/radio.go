package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/the-it-dept/rfspager/internal/radio"
)

// --- P25 radio: talkgroups, units, sites ---
//
// Rows are created by what receivers hear (internal/radio); admins only name
// and place them, so there is no create or delete. These tables have no
// updated_at, hence updateRadioRow rather than updateRow.

var (
	radioTalkgroupFields = fieldSet("alpha_tag", "description", "agency", "category", "hidden")
	radioUnitFields      = fieldSet("alias", "agency")
	radioSiteFields      = fieldSet("name", "lat", "lng")
)

func (s *Store) ListRadioTalkgroups(ctx context.Context, p ListParams) (*PaginatedResult, error) {
	return s.paginatedQuery(ctx, "radio_talkgroups", "*", "1=1", nil, p,
		[]string{"tgid", "alpha_tag", "description", "agency", "category"})
}

func (s *Store) GetRadioTalkgroup(ctx context.Context, id int64) (map[string]any, error) {
	return s.getRow(ctx, "radio_talkgroups", id, "")
}

func (s *Store) UpdateRadioTalkgroup(ctx context.Context, id int64, data map[string]any) error {
	return s.updateRadioRow(ctx, "radio_talkgroups", radioTalkgroupFields, id, data)
}

func (s *Store) ListRadioUnits(ctx context.Context, p ListParams) (*PaginatedResult, error) {
	return s.paginatedQuery(ctx, "radio_units", "*", "1=1", nil, p,
		[]string{"rid", "alias", "agency"})
}

func (s *Store) GetRadioUnit(ctx context.Context, id int64) (map[string]any, error) {
	return s.getRow(ctx, "radio_units", id, "")
}

func (s *Store) UpdateRadioUnit(ctx context.Context, id int64, data map[string]any) error {
	return s.updateRadioRow(ctx, "radio_units", radioUnitFields, id, data)
}

func (s *Store) ListRadioSites(ctx context.Context, p ListParams) (*PaginatedResult, error) {
	return s.paginatedQuery(ctx, "radio_sites", "*", "1=1", nil, p,
		[]string{"name", "site", "rfss"})
}

func (s *Store) GetRadioSite(ctx context.Context, id int64) (map[string]any, error) {
	return s.getRow(ctx, "radio_sites", id, "")
}

func (s *Store) UpdateRadioSite(ctx context.Context, id int64, data map[string]any) error {
	return s.updateRadioRow(ctx, "radio_sites", radioSiteFields, id, data)
}

// updateRadioRow updates the allowed columns of one radio row. An empty
// string clears a text column (they are all nullable), which is what an
// admin form sends for a blanked field. Returns sql.ErrNoRows for no row.
func (s *Store) updateRadioRow(ctx context.Context, table string, allowed map[string]struct{}, id int64, data map[string]any) error {
	if len(data) == 0 {
		return errors.New("nothing to update")
	}
	if err := validateFields(data, allowed); err != nil {
		return err
	}
	sets := make([]string, 0, len(data))
	vals := make([]any, 0, len(data)+1)
	for k, v := range data {
		if str, ok := v.(string); ok && strings.TrimSpace(str) == "" {
			v = nil
		}
		vals = append(vals, v)
		sets = append(sets, fmt.Sprintf(`"%s" = $%d`, k, len(vals)))
	}
	vals = append(vals, id)
	res, err := s.db.ExecContext(ctx,
		fmt.Sprintf("UPDATE %s SET %s WHERE id = $%d", table, strings.Join(sets, ", "), len(vals)), vals...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// --- Handlers ---

func (h *Handlers) ListRadioTalkgroups(w http.ResponseWriter, r *http.Request) {
	h.listRadio(w, r, h.store.ListRadioTalkgroups)
}

func (h *Handlers) GetRadioTalkgroup(w http.ResponseWriter, r *http.Request) {
	h.getRadio(w, r, h.store.GetRadioTalkgroup)
}

func (h *Handlers) UpdateRadioTalkgroup(w http.ResponseWriter, r *http.Request) {
	h.updateRadio(w, r, h.store.UpdateRadioTalkgroup)
}

func (h *Handlers) ListRadioUnits(w http.ResponseWriter, r *http.Request) {
	h.listRadio(w, r, h.store.ListRadioUnits)
}

func (h *Handlers) GetRadioUnit(w http.ResponseWriter, r *http.Request) {
	h.getRadio(w, r, h.store.GetRadioUnit)
}

func (h *Handlers) UpdateRadioUnit(w http.ResponseWriter, r *http.Request) {
	h.updateRadio(w, r, h.store.UpdateRadioUnit)
}

func (h *Handlers) ListRadioSites(w http.ResponseWriter, r *http.Request) {
	h.listRadio(w, r, h.store.ListRadioSites)
}

func (h *Handlers) GetRadioSite(w http.ResponseWriter, r *http.Request) {
	h.getRadio(w, r, h.store.GetRadioSite)
}

func (h *Handlers) UpdateRadioSite(w http.ResponseWriter, r *http.Request) {
	h.updateRadio(w, r, h.store.UpdateRadioSite)
}

// ImportRadioTalkgroups: POST /radio/talkgroups/import?sysid=721
//
// Body is CSV, either raw or as the "file" field of a multipart form, with a
// header row naming tgid,alpha_tag,description,agency,category (tgid
// required, the rest optional, any order). Names are upserted; empty cells
// leave the stored value, and counters / hidden are never touched. sysid
// defaults to the NSW PSN (0x2D1). Bad lines are skipped and listed in
// "errors"; the rest still import.
func (h *Handlers) ImportRadioTalkgroups(w http.ResponseWriter, r *http.Request) {
	sysid := radio.DefaultSysID
	if raw := r.URL.Query().Get("sysid"); raw != "" {
		v, err := strconv.ParseInt(raw, 0, 32) // accepts 721 or 0x2D1
		if err != nil || v < 1 || v > 0xFFF {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid sysid"})
			return
		}
		sysid = int(v)
	}

	var body io.Reader = r.Body
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		file, _, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "multipart upload needs a \"file\" field"})
			return
		}
		defer file.Close()
		body = file
	}

	rows, problems, err := radio.ParseTalkgroupCSV(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	result := radio.ImportResult{Skipped: len(problems), Errors: problems}
	if result.Errors == nil {
		result.Errors = []string{}
	}
	if len(rows) > 0 {
		result.Inserted, result.Updated, err = h.radio.ImportTalkgroups(r.Context(), sysid, rows)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) listRadio(w http.ResponseWriter, r *http.Request, list func(context.Context, ListParams) (*PaginatedResult, error)) {
	result, err := list(r.Context(), parseListParams(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) getRadio(w http.ResponseWriter, r *http.Request, get func(context.Context, int64) (map[string]any, error)) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	row, err := get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (h *Handlers) updateRadio(w http.ResponseWriter, r *http.Request, update func(context.Context, int64, map[string]any) error) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	data := decodeBody(r)
	if data == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if err := update(r.Context(), id, data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
