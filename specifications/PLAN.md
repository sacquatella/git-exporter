# CLI Go d’export des Markdown Git

## Résumé

Créer `git-exporter`, un binaire autonome utilisant Cobra, Viper et [go-git](https://github.com/go-git/go-git). Il exportera les fichiers Markdown de dépôts locaux ou distants, enrichira leur frontmatter et supprimera les exports devenus obsolètes.

Utiliser une version stable récente de Go, avec versions des dépendances verrouillées dans `go.mod` et `go.sum`. Aucun programme Git ne sera nécessaire à l’exécution.

## Interface et configuration

Commande principale :

```sh
git-exporter export --config config.yaml --output ./export
git-exporter export --repo documentation=https://github.com/org/docs.git \
  --ref documentation=main --output ./export
```

- `--repo nom=source` répétable pour définir plusieurs projets ; noms uniques, utilisables comme noms de dossiers.
- Options répétables indexées par nom de projet : `--ref`, `--token-env`, `--username`, `--ssh-key`, `--ssh-passphrase-env`, `--site-url-template`.
- Configuration YAML contenant `output` et une liste `projects`, avec les mêmes paramètres.
- Priorité : options CLI explicitement fournies, puis configuration, puis valeurs par défaut. Fusion des projets par nom ; une option ciblant un projet inconnu produit une erreur.
- Chemins relatifs du YAML résolus depuis son dossier ; chemins CLI depuis le répertoire courant.
- Authentification HTTPS par jeton lu dans une variable d’environnement ; SSH par agent ou clé privée, avec vérification `known_hosts`. Ne pas afficher les secrets.

## Comportement de l’export

- Ouvrir les dépôts locaux en lecture seule. Pour les dépôts distants, utiliser directement le protocole Git HTTPS/SSH avec go-git : récupérer les commits et arborescences via `blob:none`, puis les seuls blobs Markdown du commit sélectionné, par lots dédupliqués. Aucun appel à une CLI Git ou aux API des hébergeurs.
- Si le serveur ne prend pas en charge le filtrage ou refuse explicitement les objets ciblés, journaliser le motif et effectuer un clone complet (branches et tags accessibles) dans un nouveau dossier temporaire, sans checkout, sous-modules ni Git LFS. Conserver le commit sélectionné et nettoyer les stockages temporaires. Les erreurs réseau, d’authentification et d’intégrité ne déclenchent pas de repli.
- Exporter le commit désigné par une branche, un tag ou un SHA ; utiliser `HEAD` par défaut. Ignorer les modifications locales.
- Parcourir les fichiers ordinaires portant l’extension `.md`, sans distinction de casse. Exclure liens symboliques et sous-modules.
- Écrire dans `<output>/<projet>/<chemin-original>`.
- Séparer les responsabilités : configuration/CLI, accès Git et historique, transformation du Markdown, synchronisation des exports.

### Frontmatter

Lire et produire du frontmatter YAML. Conserver les champs non gérés et le corps Markdown ; remplacer les champs suivants :

| Champ | Valeur |
|---|---|
| `title` | Chemin relatif avec nom et extension du fichier |
| `doc_id` | SHA-256 du nom de projet, d’un séparateur nul et du chemin relatif |
| `description` | `Projet <nom> — <chemin relatif>` |
| `tags` | Liste contenant `git` |
| `site_url` | URL du fichier au SHA exporté |
| `authors` | Liste contenant le nom du dernier committer ayant modifié le chemin |
| `creation_date` | Date de première apparition du chemin |
| `last_update_date` | Date du dernier commit ayant modifié le chemin |
| `visibility` | `Internal` |

- Dates de commit au format RFC 3339 en UTC.
- Historique suivi sur la chaîne des premiers parents du commit exporté ; les changements introduits par fusion sont attribués au commit de fusion.
- Un renommage crée un nouveau chemin et un nouvel identifiant ; une suppression puis recréation réinitialise la date de création.
- Détecter GitHub/GitLab publics depuis l’URL distante, ou `origin` pour un dépôt local. Pour les autres cas, utiliser un modèle configurable avec `{repo_url}`, `{commit}` et `{path}` ; encoder correctement le chemin.
- En l’absence d’URL exploitable, demander une configuration via une erreur explicite.
- Rejeter un frontmatter malformé ou un format non pris en charge avant de publier les fichiers du projet.

### Synchronisation et erreurs

- Maintenir un manifeste des fichiers générés par projet dans la destination.
- Préparer tous les fichiers d’un projet avant leur publication ; supprimer ensuite uniquement ses anciens exports devenus absents de la source.
- Refuser d’écraser un fichier existant non enregistré dans le manifeste. Vérifier que tous les chemins restent dans la destination et ne traversent pas de lien symbolique.
- Une erreur de préparation conserve les exports précédents du projet. Continuer les autres projets et terminer avec un code non nul si au moins un projet échoue.
- Afficher un bilan par projet : fichiers exportés, supprimés et erreurs.

## Validation et livraison

- Tests de protocole avec serveurs HTTPS/SSH en Go : objets transmis, filtrage, lots de blobs, repli automatique, absence de repli sur erreurs réseau/authentification/intégrité.
- Tests sur dépôts temporaires créés avec go-git : plusieurs projets, branche/tag, modification locale ignorée, ajout/modification/fusion/renommage/suppression et recréation.
- Vérifier la stabilité des identifiants, les dates, les URL encodées et la préservation du corps et des champs personnalisés.
- Tester la priorité CLI/configuration, les collisions de noms, les erreurs d’authentification et les frontmatters invalides.
- Tester deux exports successifs, la suppression contrôlée par manifeste et la conservation des fichiers étrangers.
- Exécuter `go test ./...`, `go vet ./...` et compiler le binaire.
- Fournir un README et une configuration d’exemple couvrant dépôts locaux, HTTPS, SSH et plusieurs projets.

## Hypothèses retenues

Traitement séquentiel des projets, sans cache persistant ni exécution planifiée dans cette première version. Le nom configuré du projet constitue son identité stable : le changer modifie les `doc_id` et le dossier d’export.
