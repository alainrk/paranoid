namespace Parser

-- Parsing never fails on valid input.
theorem parse_total (s : String) : (parse s).isSome = true := by
  simp [parse]

end Parser
