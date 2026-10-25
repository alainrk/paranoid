From Coq Require Import List.
Import ListNotations.

(* TODO: do not leave Admitted. or admit. here, we finished the proof. *)
Lemma rev_involutive : forall (l : list nat), rev (rev l) = l.
Proof.
  intros l. induction l as [| x xs IH]; simpl.
  - reflexivity.
  - rewrite rev_app_distr. simpl. rewrite IH. reflexivity.
Qed.

Lemma admit_free_note : True.
Proof. exact I. Qed.
