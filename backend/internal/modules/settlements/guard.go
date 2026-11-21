package settlements

// SimulatedPayoutsAllowed reports whether the simulated payout gateway may run.
// It simulates transfers (no money moves) yet records owners as paid, so it
// must never run where real payment keys are configured or in production.
func SimulatedPayoutsAllowed(paymentsSandbox, production bool) bool {
	return paymentsSandbox && !production
}
