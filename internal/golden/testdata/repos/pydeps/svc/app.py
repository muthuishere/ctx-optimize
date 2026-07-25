"""A tiny module so the repo has code as well as manifests."""


def create_app():
    return configure(name="pydeps")


def configure(name):
    return {"name": name}
