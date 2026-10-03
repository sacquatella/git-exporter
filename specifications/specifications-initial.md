# Spécifications

Je voudrais ecrire un programme cli en go qui va rechercher tout les fichiers .md d'un projet git. Il récupère ces fichiers .md les posent dans un répertoire. Lors de la recuperation il ajoute dans le frontmatter des fichiers markdown les attributs  suivant

```markdown
title: <repertoire+nom_du_fichier>
doc_id: <id_unique_base_nom_et_repertoire>
description: <projet git , repertoire et nom du fichier>
tags: 
- git
site_url: <url_du_fichier_dans_projet_git> 
authors: 
- <last commiter>
creation_date: <date_creation_fichier>
last_update_date: <date_dernier_update>
visibility: Internal
```

Je voudrais utiliser les lib:

- viper
- cobra

Je veux que le cli puis recupérer les markdown d'un ou plusieur projets git.
les projets gits doivent pouvoir être passé en paramètre (avec leurs paramètres de connection) depuis le cli ou defini dans un fichier de configuration.
le repertoire cible doit pouvoir étre passé en paramètre ou défini dans le fichier de configuration.

