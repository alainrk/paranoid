namespace Parser

theorem parse_total (s : String) : s.isEmpty = true ∨ (parse s).isSome = true := by
  simp [parse]

end Parser
