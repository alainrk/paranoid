let push x s = x :: s

(* Only the single-element case is handled now. *)
let pop = function
  | [ x ] -> Some (x, [])
  | _ -> None
