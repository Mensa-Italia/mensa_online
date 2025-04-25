package area32

import "testing"

func TestScraper(t *testing.T) {
	api := NewAPI()
	main, err := api.DoLoginAndRetrieveMain("marco.montanari@mensa.it", "appleTest123")
	if err != nil {
		println(err.Error())
		return
	}
	println(main.Id)

}
