package agentjob

import (
	"errors"
	"net/http"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/agentjobs/params"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// respondIfParamsError answers a job-parameter error (ADR 0x044) and reports whether it did.
func (h *Handler) respondIfParamsError(w http.ResponseWriter, err error) bool {
	var verr *params.ValidationError
	if errors.As(err, &verr) {
		handlerutils.RespondWithErrorDetails(w, h.logger, http.StatusBadRequest, models.ErrCodeJobParametersInvalid, verr.Error(), verr, nil)
		return true
	}
	var missing *params.MissingError
	if errors.As(err, &missing) {
		respondNeedsValues(w, h, missing.Names)
		return true
	}
	return false
}

func respondNeedsValues(w http.ResponseWriter, h *Handler, names []string) {
	handlerutils.RespondWithErrorDetails(w, h.logger, http.StatusConflict, models.ErrCodeJobNeedsValues,
		"This job can't run until every parameter has a value. Missing: "+strings.Join(names, ", "),
		map[string][]string{"missing_parameters": names}, nil)
}
