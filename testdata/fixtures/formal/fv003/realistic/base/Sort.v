From Coq Require Import List Sorted Permutation.

Theorem sort_correct : forall l : list nat,
  Sorted le (sort l) /\ Permutation l (sort l).
Proof.
  intros l. split; [apply sort_sorted | apply sort_perm].
Qed.
