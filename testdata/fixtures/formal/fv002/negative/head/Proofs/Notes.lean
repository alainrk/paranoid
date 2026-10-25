/-- This proof uses no axiom and no native_decide. -/
theorem add_zero' (n : Nat) : n + 0 = n := rfl

theorem no_axiom_needed (n : Nat) : 0 + n = n := by
  simp

theorem unsafe_free (a : Nat) : a = a := rfl
