namespace Parser

/-- Parsing always yields a value. -/
theorem parse_total (s : String) :
    (parse s).isSome = true := by
  unfold parse
  simp

end Parser
