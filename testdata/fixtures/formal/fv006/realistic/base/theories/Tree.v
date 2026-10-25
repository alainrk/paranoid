Inductive tree := Leaf | Node (l r : tree).

Fixpoint size (t : tree) : nat :=
  match t with
  | Leaf => 0
  | Node l r => S (size l + size r)
  end.
