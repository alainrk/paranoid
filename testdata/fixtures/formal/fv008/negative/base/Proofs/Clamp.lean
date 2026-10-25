theorem clamp_le (x hi : Nat) : clamp x hi ≤ hi := by
  unfold clamp
  omega
