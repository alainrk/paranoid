import Mathlib.Data.List.Basic

namespace Proofs

/-- Reversing twice gives back the list. -/
theorem rev_rev (l : List α) : l.reverse.reverse = l := by
  induction l with
  | nil => rfl
  | cons x xs ih =>
    simp [List.reverse_cons, ih]

end Proofs
