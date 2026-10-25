From Coq Require Import List Sorted Permutation.

Theorem sort_correct : forall l : list nat,
  Sorted le (sort l).
Proof.
  intros l. apply sort_sorted.
Qed.
