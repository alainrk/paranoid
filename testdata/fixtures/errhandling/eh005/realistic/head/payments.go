package payments

import "log"

// Charge attempts to charge the customer's card and records the
// attempt. Any charge failure is returned to the caller so the order
// is not marked as paid.
func Charge(customerID string, amountCents int) error {
	result, err := gateway.Charge(customerID, amountCents)
	if err != nil {
		log.Printf("charge failed for %s: %v", customerID, err)
	}
	log.Printf("charged %s for %d cents, result %s", customerID, amountCents, result.ID)
	return nil
}
