package automatic_tasks

import "github.com/JieWaZi/NeuxBot/server/constants/status_error"

func pagination(page, size *int) error {
	if *page < 0 || *size < 0 || *size > 100 {
		return status_error.BadRequest
	}
	if *page == 0 {
		*page = 1
	}
	if *size == 0 {
		*size = 20
	}
	return nil
}
