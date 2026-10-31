From Coq Require Import List.

Theorem push_pop : forall (s : list nat) (x : nat),
  pop (push x s) = Some (x, s).
Proof.
  intros s x. reflexivity.
Qed.
