package area32

import "testing"

func TestScraper(t *testing.T) {
	api := NewAPI()
	main, err := api.DoLoginAndRetrieveMain("redacted@example.com", "REDACTED-CREDENTIAL")
	if err != nil {
		println(err.Error())
		return
	}
	println(main.Id)

}
