
DSNExec
-------

DSNExec is a cli tool that watches files and then reacts to those changes
by executing commands. The commands can be SQL commands or they can be
shell commands.

Configuration
-------------

Sources:
    driver: The driver is used for parsing the source file. The parser allows 
            the content of the file to be referenced in a template. There are
            several drivers available.

        json:     a json parser
        yaml:     a yaml parser
        postgres: a postgres connection string parser
        none:     does not try to parse the input file
    
    filename: the name of the file to use as a source.

Destination:
    driver: A database driver to execute the commands with. This is used
            with the golang sql package. A "shelldb" driver also is registed
            so that any executable can be run with the same abstraction.

    dsn:    A DSN for the driver to connect with.

Commands:
    command:    A SQL command to exectute. This sql command is parsed with
                the golang text/template package with the sources map as the
                template context.

    args:       An array of arguments to the SQL command. The args are parsed with
                the golang text/template package with the sources map as the
                template context. 
                (Note - If you intent to fire DDL queries, then put full go template expressions in the command. 
                As unlike for DML queries, underlying prepare statement does not consider arguments passed when the query is DDL.)

