From Coq Require Import List.
Import ListNotations.

Lemma rev_involutive : forall (l : list nat), rev (rev l) = l.
Proof.
  intros l. induction l as [| x xs IH]; simpl.
  - reflexivity.
  - rewrite rev_app_distr. simpl. rewrite IH. reflexivity.
Qed.
