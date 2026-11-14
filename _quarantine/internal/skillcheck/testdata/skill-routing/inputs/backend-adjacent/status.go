package fixture

import "errors"

var ErrMissing = errors.New("missing")

func HTTPStatus(err error) int {
	if err == nil {
		return 204
	}
	if errors.Is(err, ErrMissing) {
		return 500
	}
	return 500
}
