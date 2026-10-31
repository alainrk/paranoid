From Coq Require Import List Arith.

Lemma heap_insert_size : forall h x, size (insert x h) = S (size h).
Proof.
  intros. induction h; simpl; auto.
Qed.
