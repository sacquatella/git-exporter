# git-exporter

CLI Go qui exporte les fichiers Markdown de plusieurs dépôts Git et enrichit leur frontmatter YAML. Le binaire fonctionne sans programme Git installé et sans API GitHub/GitLab.

## Compiler et lancer

Go **1.25 ou plus récent** est requis pour compiler. Les dépendances sont verrouillées dans `go.mod` et `go.sum`.

```sh
go build -o bin/git-exporter .
./bin/git-exporter export --repo docs=https://github.com/organisation/docs.git --output ./export
./bin/git-exporter export --config config.example.yaml
./bin/git-exporter export --help
```

Le fichier d’exemple contient des adresses fictives à remplacer. Le binaire compilé n’a pas besoin de Go à l’exécution.

## Configuration

```yaml
output: ./export
projects:
  - name: docs
    source: https://github.com/organisation/docs.git
    ref: main
    token_env: DOCS_TOKEN
  - name: local
    source: ../mon-projet
    site_url_template: "https://git.example.com/equipe/projet/blob/{commit}/{path}"
```

`name` est un identifiant stable, unique, composé de lettres ASCII, chiffres, points, tirets et underscores, commençant par une lettre ou un chiffre. Il détermine le sous-dossier d’export et entre dans le calcul des identifiants des documents.

Les options explicitement passées en CLI remplacent les valeurs YAML. Les projets sont fusionnés par nom ; `--repo` ajoute un projet ou remplace sa source. Une option ciblant un projet inconnu est une erreur. Les chemins du YAML sont relatifs au fichier de configuration ; ceux du CLI sont relatifs au répertoire courant. Aucun fichier de configuration n’est chargé implicitement.

| Option répétable | Champ YAML | Exemple |
| --- | --- | --- |
| `--repo` | `name`, `source` | `--repo docs=../docs` |
| `--ref` | `ref` | `--ref docs=main` |
| `--username` | `username` | `--username docs=git` |
| `--token-env` | `token_env` | `--token-env docs=DOCS_TOKEN` |
| `--ssh-key` | `ssh_key` | `--ssh-key docs=./keys/id_ed25519` |
| `--ssh-passphrase-env` | `ssh_passphrase_env` | `--ssh-passphrase-env docs=KEY_PASSPHRASE` |
| `--site-url-template` | `site_url_template` | `--site-url-template 'docs={repo_url}/-/blob/{commit}/{path}'` |

`--output` ou `output` est obligatoire. La référence est une branche, un tag ou un SHA de commit complet. Par défaut : `HEAD` local, branche par défaut distante. Les modifications locales et fichiers non suivis sont ignorés.

## Connexion

- **HTTPS** : `token_env` nomme une variable d’environnement contenant le jeton ; `username` vaut `git` par défaut. Aucun jeton n’est nécessaire pour un dépôt public. Les mots de passe intégrés à une URL sont refusés.
- **SSH** : URL `ssh://git@serveur/chemin.git` ou `git@serveur:chemin.git`. Utilisation d’une clé `ssh_key`, éventuellement chiffrée via `ssh_passphrase_env`, ou de l’agent SSH si aucune clé n’est configurée.
- La clé du serveur SSH est vérifiée via `known_hosts`. `SSH_KNOWN_HOSTS` permet de désigner un fichier spécifique ; sinon les emplacements usuels de go-git sont utilisés. TLS est vérifié pour HTTPS.
- Les erreurs affichées masquent les valeurs des variables de jeton et de passphrase configurées. Les programmes externes de gestion des identifiants Git ne sont pas exécutés.

## Récupération distante

Le CLI utilise directement le protocole Git :

1. Résolution de la référence et fixation du commit à exporter.
2. Requête filtrée `blob:none` pour les commits et arborescences accessibles depuis ce commit.
3. Découverte des fichiers `.md` (extension insensible à la casse).
4. Récupération des seuls blobs Markdown du commit sélectionné, dédupliqués et regroupés par lots de 128.

Les contenus non Markdown et anciennes versions des fichiers ne sont pas demandés. Les métadonnées d’historique peuvent néanmoins représenter un volume important, notamment sur les dépôts anciens avec de nombreuses branches fusionnées.

Si le serveur n’annonce pas le filtre ou refuse explicitement le filtrage ou la demande ciblée d’objets, le CLI journalise le motif, abandonne le stockage partiel et effectue un **clone complet** dans un nouveau dossier temporaire. Ce clone récupère toutes les branches et les tags accessibles, sans checkout, sous-modules ni objets Git LFS. Le commit initialement sélectionné reste celui exporté.

Les erreurs réseau, d’authentification, de clé SSH, d’annulation et de lecture/intégrité du pack sont remontées sans repli. Tous les stockages temporaires sont nettoyés à la fin. Aucun cache persistant n’est conservé.

## Documents générés

Les fichiers ordinaires sont écrits dans `<output>/<projet>/<chemin-original>`. Les liens symboliques et sous-modules sont exclus. Les images, pièces jointes et liens relatifs présents dans le corps Markdown ne sont pas copiés ni réécrits. Un pointeur LFS suivi comme fichier `.md` reste son contenu Git ; aucun objet LFS n’est téléchargé.

| Champ | Valeur générée |
| --- | --- |
| `title` | Chemin relatif complet, extension comprise |
| `doc_id` | SHA-256 de `nom-projet + NUL + chemin-relatif` |
| `description` | `Projet <nom> — <chemin relatif>` |
| `tags` | Liste contenant `git` |
| `site_url` | URL vers le fichier au SHA exporté |
| `authors` | Liste contenant le nom du dernier committer |
| `creation_date` | Date d’apparition du chemin |
| `last_update_date` | Date de sa dernière modification |
| `visibility` | `Internal` |

Les dates utilisent l’horodatage du **committer**, en RFC 3339 UTC. L’historique suit les premiers parents : un changement introduit par fusion est attribué au commit de fusion. Un renommage crée une nouvelle identité ; une suppression suivie d’une recréation réinitialise la date de création. Un historique local incomplet provoque une erreur plutôt qu’une date inventée.

Les champs gérés sont remplacés. Les autres valeurs YAML sont conservées, y compris celles provenant d’alias ; leur présentation et leurs commentaires peuvent être normalisés. Le corps Markdown est conservé octet pour octet. Seul le frontmatter YAML délimité par `---` est pris en charge ; les frontmatters invalides, TOML ou JSON sont rejetés.

Les URL publiques GitHub et GitLab sont déduites de la source distante ou de `origin` pour un dépôt local. Pour une instance privée ou un autre hébergeur, fournir `site_url_template` avec `{repo_url}`, `{commit}`, `{path}`. `{repo_url}` est l’URL HTTPS du dépôt sans identifiants ni suffixe `.git`. Le chemin du document est encodé pour une URL. Un modèle absolu sans `{repo_url}` fonctionne aussi pour un dépôt local sans remote.

## Réexports et erreurs

Chaque sous-dossier de projet contient `.git-exporter-manifest.json`, qui recense ses exports. Un réexport met à jour les fichiers actuels et supprime uniquement les anciens fichiers enregistrés devenus absents. Les fichiers étrangers sont conservés ; une collision est une erreur. Ne pas supprimer ni modifier le manifeste pour un export existant.

Tous les contenus et chemins sont validés et préparés avant publication. Une erreur de préparation laisse les fichiers précédents intacts. Les écritures sont publiées par renommages individuels ; une panne système pendant la publication n’est pas une transaction atomique à l’échelle du projet. Les ancêtres symboliques et chemins sortant de la destination sont refusés. Un verrou `.git-exporter.lock` empêche deux publications simultanées : après un arrêt brutal, supprimer ce dossier seulement après avoir vérifié qu’aucun export n’est encore actif.

Les projets sont traités séquentiellement. Un échec n’empêche pas les projets suivants de s’exécuter. Le code de sortie vaut 0 si tous réussissent, 1 sinon. Le bilan indique le mode `local`, `sélectif` ou `complet`, ainsi que les nombres de fichiers exportés et supprimés.

## Vérification

```sh
go test ./...
go vet ./...
go build -o bin/git-exporter .
```

Les tests créent des dépôts, des packs et des serveurs Git HTTPS/SSH en Go, sans exécuter la CLI Git ni contacter un hébergeur. Ils couvrent les objets transmis, le repli, les métadonnées, les lots de blobs, la vérification SSH, la configuration, le frontmatter et les suppressions contrôlées.
