let push x s = x :: s

let pop = function
  | [] -> None
  | x :: rest -> Some (x, rest)
