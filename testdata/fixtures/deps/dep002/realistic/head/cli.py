import click
import fresh_hotness

@click.command()
def main():
    click.echo(fresh_hotness.greet())
