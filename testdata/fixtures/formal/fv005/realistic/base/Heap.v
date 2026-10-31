From Coq Require Import List Arith.

Lemma heap_insert_size : forall h x, size (insert x h) = S (size h).
Proof.
  intros. induction h; simpl; auto.
Qed.

Lemma heap_merge_comm : forall a b, size (merge a b) = size (merge b a).
Proof.
  intros. apply merge_size_sym.
Qed.
