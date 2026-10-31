theorem good_add (a b : Nat) : a + b = b + a := by
  exact Classical.byContradiction (fun h => h (by omega))
