From Coq Require Import List.

Theorem push_pop : forall (s : list nat) (x : nat),
  pop (push x s) <> None.
Proof.
  intros s x. discriminate.
Qed.
