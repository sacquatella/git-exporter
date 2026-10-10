# Specifications

I would like to write a Go CLI program that finds all .md files in a Git project. It retrieves these .md files and places them in a directory. During retrieval, it adds the following attributes to the Markdown files' frontmatter:

```markdown
title: <directory+filename>
doc_id: <unique_id_based_on_name_and_directory>
description: <git_project_directory_and_filename>
tags:
- git
site_url: <file_url_in_git_project>
authors:
- <last committer>
creation_date: <file_creation_date>
last_update_date: <last_update_date>
visibility: Internal
```

I would like to use these libraries:

- viper
- cobra

The CLI should retrieve Markdown files from one or more Git projects.
Git projects and their connection settings must be configurable through CLI arguments or a configuration file.
The output directory must be configurable through a CLI argument or the configuration file.
